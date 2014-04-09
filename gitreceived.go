package main

import (
	"bytes"
	"crypto/md5"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/flynn/go-crypto-ssh"
	"github.com/flynn/go-shlex"
)

const PrereceiveHookTmpl = `#!/bin/bash
set -eo pipefail; while read oldrev newrev refname; do
[[ $refname = "refs/heads/master" ]] && git archive $newrev | {{RECEIVER}} "$RECEIVE_REPO" "$newrev" | sed -$([[ $(uname) == "Darwin" ]] && echo l || echo u) "s/^/"$'\e[1G'"/"
done
`

var prereceiveHook []byte

var port *string = flag.String("p", "22", "port to listen on")
var repoPath *string = flag.String("r", "/tmp/repos", "path to repo cache")
var noAuth *bool = flag.Bool("n", false, "disable client authentication")

var authChecker []string
var privateKey string

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:  %v [options] <privatekey> <authchecker> <receiver>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if len(os.Args) < 2 {
		flag.Usage()
		return
	}
	var err error
	privateKey = flag.Arg(0)
	authChecker, err = shlex.Split(flag.Arg(1))
	if err != nil {
		log.Fatalln("Unable to parse authchecker command:", err)
	}

	receiver, err := shlex.Split(flag.Arg(2))
	if err != nil {
		log.Fatalln("Unable to parse receiver command:", err)
	}
	receiver[0], err = filepath.Abs(receiver[0])
	if err != nil {
		log.Fatalln("Invalid receiver path:", err)
	}
	prereceiveHook = []byte(strings.Replace(PrereceiveHookTmpl, "{{RECEIVER}}", strings.Join(receiver, " "), 1))

	var config *ssh.ServerConfig
	if *noAuth {
		config = &ssh.ServerConfig{NoClientAuth: true}
	} else {
		config = &ssh.ServerConfig{PublicKeyCallback: func(*ssh.ServerConn, string, string, []byte) bool { return true }}
	}

	pemBytes, err := ioutil.ReadFile(privateKey)
	if err != nil {
		log.Fatal("Failed to load private key:", err)
	}
	if err = config.SetRSAPrivateKey(pemBytes); err != nil {
		log.Fatal("Failed to parse private key:", err)
	}

	if p := os.Getenv("PORT"); p != "" && *port == "22" {
		*port = p
	}
	listener, err := ssh.Listen("tcp", "0.0.0.0:"+*port, config)
	if err != nil {
		log.Fatal("failed to listen for connection")
	}
	for {
		// SSH connections just house multiplexed connections
		conn, err := listener.Accept()
		if err != nil {
			log.Println("failed to accept incoming connection:", err)
			continue
		}
		if err := conn.Handshake(); err != nil {
			log.Println("failed to handshake:", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn *ssh.ServerConn) {
	defer conn.Close()
	for {
		// Accept reads from the connection, demultiplexes packets
		// to their corresponding channels and returns when a new
		// channel request is seen. Some goroutine must always be
		// calling Accept; otherwise no messages will be forwarded
		// to the channels.
		ch, err := conn.Accept()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Println("handleConnection Accept:", err)
			break
		}
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			break
		}
		go handleChannel(conn, ch)
	}
}

func handleChannel(conn *ssh.ServerConn, ch ssh.Channel) {
	defer ch.Close()
	if err := ch.Accept(); err != nil {
		log.Println("ch.Accept failed:", err)
		return
	}
	for {
		req, err := ch.ReadRequest()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Println("handleChannel read request err:", err)
			continue
		}
		switch req.Request {
		case "exec":
			fail := func(at string, err error) {
				log.Printf("%s failed: %s", at, err)
				ch.Stderr().Write([]byte("Internal error.\n"))
			}
			if req.WantReply {
				ch.AckRequest(true)
			}
			cmdline := string(req.Payload[4:])
			cmdargs, err := shlex.Split(cmdline)
			if err != nil || len(cmdargs) != 2 {
				ch.Stderr().Write([]byte("Invalid arguments.\n"))
				return
			}
			if cmdargs[0] != "git-receive-pack" {
				ch.Stderr().Write([]byte("Only `git push` is supported.\n"))
				return
			}
			cmdargs[1] = strings.TrimSuffix(strings.TrimPrefix(cmdargs[1], "/"), ".git")
			if strings.Contains(cmdargs[1], "/") {
				ch.Stderr().Write([]byte("Invalid repo.\n"))
				return
			}

			if !*noAuth {
				if err := checkAuth(conn.User, cmdargs[1], conn.PublicKey); err != nil {
					if err == ErrUnauthorized {
						ch.Stderr().Write([]byte("Unauthorized.\n"))
					} else {
						fail("checkAuth", err)
					}
					return
				}
			}

			if err := ensureCacheRepo(cmdargs[1]); err != nil {
				fail("ensureCacheRepo", err)
				return
			}
			var fingerprint string
			if conn.PublicKey != nil {
				fingerprint = publicKeyFingerprint(conn.PublicKey)
			}
			cmd := exec.Command("git-shell", "-c", cmdargs[0]+" '"+cmdargs[1]+"'")
			cmd.Dir = *repoPath
			cmd.Env = append(os.Environ(),
				"RECEIVE_USER="+conn.User,
				"RECEIVE_REPO="+cmdargs[1],
				"RECEIVE_FINGERPRINT="+fingerprint,
			)
			done, err := attachCmd(cmd, ch, ch.Stderr(), ch)
			if err != nil {
				fail("attachCmd", err)
				return
			}
			if err := cmd.Start(); err != nil {
				fail("cmd.Start", err)
				return
			}
			done.Wait()
			status, err := exitStatus(cmd.Wait())
			if err != nil {
				fail("exitStatus", err)
				return
			}
			ch.Exit(uint(status))
		case "env":
			if req.WantReply {
				ch.AckRequest(true)
			}
		}
	}
}

var ErrUnauthorized = errors.New("gitreceive: user is unauthorized")

func checkAuth(user, repo string, key ssh.PublicKey) error {
	keyData := string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))
	status, err := exitStatus(exec.Command(authChecker[0], append(authChecker[1:], user, repo, keyData)...).Run())
	if err != nil {
		return err
	}
	if status == 0 {
		return nil
	}
	return ErrUnauthorized
}

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(2)

	stdinIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	go func() {
		io.Copy(stdinIn, stdin)
		stdinIn.Close()
	}()
	go func() {
		io.Copy(stdout, stdoutOut)
		wg.Done()
	}()
	go func() {
		io.Copy(stderr, stderrOut)
		wg.Done()
	}()

	return &wg, nil
}

func exitStatus(err error) (int, error) {
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// There is no platform independent way to retrieve
			// the exit code, but the following will work on Unix
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus(), nil
			}
		}
		return -1, err
	}
	return 0, nil
}

var cacheMtx sync.Mutex

func ensureCacheRepo(path string) error {
	cacheMtx.Lock()
	defer cacheMtx.Unlock()

	cachePath := *repoPath + "/" + path
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		os.MkdirAll(cachePath, 0755)
		cmd := exec.Command("git", "init", "--bare")
		cmd.Dir = cachePath
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	return ioutil.WriteFile(cachePath+"/hooks/pre-receive", prereceiveHook, 0755)
}

func publicKeyFingerprint(key ssh.PublicKey) string {
	var values []string
	h := md5.New()
	h.Write(ssh.MarshalPublicKey(key))
	for _, value := range h.Sum(nil) {
		values = append(values, fmt.Sprintf("%x", value))
	}
	return strings.Join(values, ":")
}
