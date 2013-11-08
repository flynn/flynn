package main

import (
	"bytes"
	"crypto/md5"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flynn/go-crypto-ssh"
	"github.com/flynn/go-shlex"
)

const PrereceiveHook = `#!/bin/bash
set -eo pipefail; while read oldrev newrev refname; do
[[ $refname = "refs/heads/master" ]] && git archive $newrev | {{RECEIVER}} "$RECEIVE_USER" "$RECEIVE_REPO" "$RECEIVE_KEYNAME" "$RECEIVE_FINGERPRINT" | sed -$([[ $(uname) == "Darwin" ]] && echo l || echo u) "s/^/"$'\e[1G'"/"
done
`

var port *string = flag.String("p", "22", "port to listen on")
var repoPath *string = flag.String("r", "/tmp/repos", "path to repo cache")
var keyPath *string = flag.String("k", "/tmp/keys", "path to named keys")
var noAuth *bool = flag.Boolean("n", false, "no client authentication")

var receiver string
var privateKey string
var keyNames = make(map[string]string)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:  %v [options] <privatekey> <receiver>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if len(os.Args) < 2 {
		flag.Usage()
		return
	}
	privateKey = flag.Arg(0)
	receiver = flag.Arg(1)

	var config *ssh.ServerConfig
	if *noAuth {
		config = &ssh.ServerConfig{
			NoClientAuth: true,
		}
	} else {
		config = &ssh.ServerConfig{
			PublicKeyCallback: func(conn *ssh.ServerConn, user, algo string, pubkey []byte) bool {
				clientkey, _, ok := ssh.ParsePublicKey(pubkey)
				if !ok {
					return false
				}
				os.MkdirAll(*keyPath, 0755)
				files, err := ioutil.ReadDir(*keyPath)
				if err != nil {
					panic(err)
				}
				for _, file := range files {
					if !file.IsDir() {
						data, err := ioutil.ReadFile(*keyPath + "/" + file.Name())
						if err != nil {
							panic(err)
						}
						filekey, _, _, _, ok := ssh.ParseAuthorizedKey(data)
						if !ok {
							continue
						}
						if bytes.Equal(clientkey.Marshal(), filekey.Marshal()) {
							keyNames[publicKeyFingerprint(clientkey)] = file.Name()
							return true
						}
					}
				}
				return false
			},
		}
	}

	pemBytes, err := ioutil.ReadFile(privateKey)
	if err != nil {
		log.Fatal("Failed to load private key:", err)
	}
	if err = config.SetRSAPrivateKey(pemBytes); err != nil {
		log.Fatal("Failed to parse private key:", err)
	}

	listener, err := ssh.Listen("tcp", "0.0.0.0:"+*port, config)
	if err != nil {
		log.Fatal("failed to listen for connection")
	}
	for {
		// SSH connections just house multiplexed connections
		conn, err := listener.Accept()
		if err != nil {
			log.Println("failed to accept incoming connection")
			continue
		}
		if err := conn.Handshake(); err != nil {
			log.Println("failed to handshake")
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
			log.Println("hanndleConnection Accept:", err)
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
	err := ch.Accept()
	if err != nil {
		panic(err)
	}
	defer ch.Close()
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
			if req.WantReply {
				ch.AckRequest(true)
			}
			cmdline := string(req.Payload[4:])
			cmdargs, err := shlex.Split(cmdline)
			if err != nil {
				panic(err)
			}
			ensureCacheRepo(cmdargs[1])
			var keyname, fingerprint string
			if *noAuth {
				fingerprint = ""
				keyname = ""
			} else {
				fingerprint = publicKeyFingerprint(conn.PublicKey)
				keyname = keyNames[fingerprint]
			}
			cmd := exec.Command(cmdargs[0], cmdargs[1:]...)
			cmd.Dir = *repoPath
			cmd.Env = []string{
				"RECEIVE_USER=" + conn.User,
				"RECEIVE_REPO=" + cmdargs[1],
				"RECEIVE_KEYNAME=" + keyname,
				"RECEIVE_FINGERPRINT=" + fingerprint,
			}
			errCh := attachCmd(cmd, ch, ch.Stderr(), ch)
			err = cmd.Start()
			if err != nil {
				panic(err)
			}
			exitCh := exitStatusCh(cmd)
			if err = <-errCh; err != nil {
				panic(err)
			}
			ch.Exit(<-exitCh)
		case "env":
			if req.WantReply {
				ch.AckRequest(true)
			}
		}
	}
}

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer, stdin io.Reader) chan error {
	errCh := make(chan error)

	stdinIn, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdoutOut, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderrOut, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}

	go func() {
		_, e := io.Copy(stdinIn, stdin)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stdout, stdoutOut)
		errCh <- e
	}()
	go func() {
		_, e := io.Copy(stderr, stderrOut)
		errCh <- e
	}()

	return errCh
}

func exitStatusCh(cmd *exec.Cmd) chan uint {
	exitCh := make(chan uint)
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// There is no plattform independent way to retrieve
				// the exit code, but the following will work on Unix
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					exitCh <- uint(status.ExitStatus())
				}
			} else {
				panic(err)
			}
			return
		}
		exitCh <- uint(0)
	}()
	return exitCh
}

func ensureCacheRepo(path string) {
	cachePath := *repoPath + "/" + path
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		os.MkdirAll(cachePath, 0755)
		cmd := exec.Command("git", "init", "--bare")
		cmd.Dir = cachePath
		err = cmd.Run()
		if err != nil {
			panic(err)
		}
	}
	receiver, err := filepath.Abs(receiver)
	if err != nil {
		panic(err)
	}
	ioutil.WriteFile(
		cachePath+"/hooks/pre-receive",
		[]byte(strings.Replace(PrereceiveHook, "{{RECEIVER}}", receiver, 1)),
		0755)
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
