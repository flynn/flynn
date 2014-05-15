package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"code.google.com/p/go.crypto/ssh"
	"github.com/flynn/go-shlex"
)

const PrereceiveHookTmpl = `#!/bin/bash
set -eo pipefail; while read oldrev newrev refname; do
[[ $refname = "refs/heads/master" ]] && git archive $newrev | {{RECEIVER}} "$RECEIVE_REPO" "$newrev" | sed -$([[ $(uname) == "Darwin" ]] && echo l || echo u) "s/^/"$'\e[1G'"/"
done
`

var prereceiveHook []byte

var port = flag.String("p", "22", "port to listen on")
var repoPath = flag.String("r", "/tmp/repos", "path to repo cache")
var noAuth = flag.Bool("n", false, "disable client authentication")
var keys = flag.String("k", "", "pem file containing private keys (read from SSH_PRIVATE_KEYS by default)")

var authChecker []string

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <authchecker> <receiver>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(64)
	}
	var err error
	authChecker, err = shlex.Split(flag.Arg(0))
	if err != nil {
		log.Fatalln("Unable to parse authchecker command:", err)
	}

	receiver, err := shlex.Split(flag.Arg(1))
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
		config = &ssh.ServerConfig{PublicKeyCallback: checkAuth}
	}

	if keyEnv := os.Getenv("SSH_PRIVATE_KEYS"); keyEnv != "" {
		if err := parseKeys(config, []byte(keyEnv)); err != nil {
			log.Fatalln("Failed to load private keys:", err)
		}
	} else {
		pemBytes, err := ioutil.ReadFile(*keys)
		if err != nil {
			log.Fatalln("Failed to load private keys:", err)
		}
		if err := parseKeys(config, pemBytes); err != nil {
			log.Fatalln("Failed to parse private keys:", err)
		}
	}

	if p := os.Getenv("PORT"); p != "" && *port == "22" {
		*port = p
	}
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalln("Failed to listen for connections:", err)
	}
	for {
		// SSH connections just house multiplexed connections
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Failed to accept incoming connection:", err)
			continue
		}
		go handleConn(conn, config)
	}
}

func parseKeys(conf *ssh.ServerConfig, pemData []byte) error {
	var found bool
	for {
		var block *pem.Block
		block, pemData = pem.Decode(pemData)
		if block == nil {
			if !found {
				return errors.New("no private keys found")
			}
			return nil
		}
		if err := addKey(conf, block); err != nil {
			return err
		}
		found = true
	}
}

func addKey(conf *ssh.ServerConfig, block *pem.Block) (err error) {
	var key interface{}
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	case "DSA PRIVATE KEY":
		key, err = ssh.ParseDSAPrivateKey(block.Bytes)
	default:
		return fmt.Errorf("unsupported key type %q", block.Type)
	}
	if err != nil {
		return err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return err
	}
	conf.AddHostKey(signer)
	return nil
}

func handleConn(conn net.Conn, conf *ssh.ServerConfig) {
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, conf)
	if err != nil {
		log.Println("Failed to handshake:", err)
		return
	}

	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		go handleChannel(sshConn, ch)
	}
}

func handleChannel(conn *ssh.ServerConn, newChan ssh.NewChannel) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Println("newChan.Accept failed:", err)
		return
	}
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			fail := func(at string, err error) {
				log.Printf("%s failed: %s", at, err)
				ch.Stderr().Write([]byte("Internal error.\n"))
			}
			if req.WantReply {
				req.Reply(true, nil)
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

			if err := ensureCacheRepo(cmdargs[1]); err != nil {
				fail("ensureCacheRepo", err)
				return
			}
			cmd := exec.Command("git-shell", "-c", cmdargs[0]+" '"+cmdargs[1]+"'")
			cmd.Dir = *repoPath
			cmd.Env = append(os.Environ(),
				"RECEIVE_USER="+conn.User(),
				"RECEIVE_REPO="+cmdargs[1],
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
			if _, err := ch.SendRequest("exit-status", false, ssh.Marshal(&status)); err != nil {
				fail("sendExit", err)
			}
			return
		case "env":
			if req.WantReply {
				req.Reply(true, nil)
			}
		}
	}
}

var ErrUnauthorized = errors.New("gitreceive: user is unauthorized")

func checkAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	status, err := exitStatus(exec.Command(authChecker[0],
		append(authChecker[1:], conn.User(), string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))))...).Run())
	if err != nil {
		return nil, err
	}
	if status.Status == 0 {
		return nil, nil
	}
	return nil, ErrUnauthorized
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

type exitStatusMsg struct {
	Status uint32
}

func exitStatus(err error) (exitStatusMsg, error) {
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// There is no platform independent way to retrieve
			// the exit code, but the following will work on Unix
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return exitStatusMsg{uint32(status.ExitStatus())}, nil
			}
		}
		return exitStatusMsg{0}, err
	}
	return exitStatusMsg{0}, nil
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
