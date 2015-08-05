package main

import (
	"archive/tar"
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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-shlex"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/flynn/flynn/pkg/archiver"
	"github.com/flynn/flynn/pkg/status"
)

const PrereceiveHookTmpl = `#!/bin/bash
set -eo pipefail; while read oldrev newrev refname; do
[[ $refname = "refs/heads/master" ]] && git archive $newrev | {{RECEIVER}} "$RECEIVE_REPO" "$newrev" | sed -$([[ $(uname) == "Darwin" ]] && echo l || echo u) "s/^/"$'\e[1G\e[K'"/"
done
`

var prereceiveHook []byte

var port = flag.String("p", "22", "port to listen for SSH on")
var httpPort = flag.String("http-port", "5000", "port to listen for HTTP on")
var useBlobstore = flag.Bool("b", true, "use the blobstore for repo cache")
var repoPath = flag.String("r", "/tmp/repos", "path to repo cache")
var noAuth = flag.Bool("n", false, "disable client authentication")
var keys = flag.String("k", "", "pem file containing private keys (read from SSH_PRIVATE_KEYS by default)")
var cacheKeyHook = flag.String("cache-key-hook", "", "hook to run to determine the cache key (optional)")

var authChecker = flag.String("auth-checker", "", "path to an executable that will check if the key is authorized")
var receiver = flag.String("receiver", "", "path to an executable that will handle the push")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if *authChecker == "" {
		log.Fatalln("Missing authchecker command.")
	}
	if *receiver == "" {
		log.Fatalln("Missing receiver command.")
	}
	var err error
	*receiver, err = filepath.Abs(*receiver)
	if err != nil {
		log.Fatalln("Invalid receiver path:", err)
	}
	prereceiveHook = []byte(strings.Replace(PrereceiveHookTmpl, "{{RECEIVER}}", *receiver, 1))

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
	go startHTTPServer()
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

func startHTTPServer() {
	if p := os.Getenv("PORT_1"); p != "" && *httpPort == "5000" {
		*httpPort = p
	}
	status.AddHandler(status.HealthyHandler)
	log.Fatal(http.ListenAndServe(":"+*httpPort, nil))
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
			var cmdline struct{ Value string }
			ssh.Unmarshal(req.Payload, &cmdline)
			cmdargs, err := shlex.Split(cmdline.Value)
			if err != nil || len(cmdargs) != 2 {
				ch.Stderr().Write([]byte("Invalid arguments.\n"))
				return
			}
			if cmdargs[0] != "git-receive-pack" {
				ch.Stderr().Write([]byte("Only `git push` is supported.\n"))
				return
			}
			cmdargs[1] = strings.TrimSuffix(strings.TrimPrefix(cmdargs[1], "/"), ".git")
			if strings.Contains(cmdargs[1], "..") {
				ch.Stderr().Write([]byte("Invalid repo.\n"))
				return
			}

			cacheKey := cmdargs[1]
			if *cacheKeyHook != "" {
				var result bytes.Buffer
				var errout bytes.Buffer
				cmd := exec.Command(*cacheKeyHook, cmdargs[0], cmdargs[1])
				cmd.Stdout = &result
				cmd.Stderr = &errout
				if err := cmd.Run(); err != nil {
					fail("cacheKeyHook", errors.New(errout.String()))
					return
				}
				cacheKey = strings.TrimSpace(result.String())
			}

			tempDir := *repoPath
			if *useBlobstore {
				path, err := ioutil.TempDir("", "")
				if err != nil {
					fail("generateTempDir", err)
					return
				}
				defer os.RemoveAll(path)
				tempDir = path
			}
			if err := ensureCacheRepo(tempDir, cacheKey); err != nil {
				fail("ensureCacheRepo", err)
				return
			}
			if *useBlobstore {
				if err := restoreBlobstoreCache(tempDir, cacheKey); err != nil {
					fail("restoreBlobstoreCache", err)
					return
				}
			}
			cmd := exec.Command("git-shell", "-c", cmdargs[0]+" '"+cacheKey+"'")
			cmd.Dir = tempDir
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
			if *useBlobstore {
				if err := uploadCache(tempDir, cacheKey); err != nil {
					fail("uploadCache", err)
				}
			}
			if _, err := ch.SendRequest("exit-status", false, ssh.Marshal(&status)); err != nil {
				fail("sendExit", err)
				return
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
	cmd := exec.Command(*authChecker,
		conn.User(),
		string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key))),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	status, err := exitStatus(cmd.Run())
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

func blobstoreCacheURL(cacheKey string) string {
	return fmt.Sprintf("http://blobstore.discoverd/repos/%s.tar", cacheKey)
}

func restoreBlobstoreCache(tempDir, path string) error {
	cachePath := filepath.Join(tempDir, path)

	res, err := http.Get(blobstoreCacheURL(path))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == 200 {
		// cache hit
		r := tar.NewReader(res.Body)
		return archiver.Untar(cachePath, r)
	}
	return nil
}

func ensureCacheRepo(tempDir, path string) error {
	cacheMtx.Lock()
	defer cacheMtx.Unlock()

	cachePath := filepath.Join(tempDir, path)
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		os.MkdirAll(cachePath, 0755)
		cmd := exec.Command("git", "init", "--bare")
		cmd.Dir = cachePath
		err = cmd.Run()
		if err != nil {
			return err
		}
		return ioutil.WriteFile(filepath.Join(cachePath, "hooks", "pre-receive"), prereceiveHook, 0755)
	}
	return nil
}

func uploadCache(tempDir, path string) error {
	cachePath := filepath.Join(tempDir, path)

	r, w := io.Pipe()
	tw := tar.NewWriter(w)

	errCh := make(chan error)
	go func() {
		err := archiver.Tar(cachePath, tw)
		tw.Close()
		w.Close()
		errCh <- err
	}()

	// upload the tarball to the blobstore
	req, err := http.NewRequest("PUT", blobstoreCacheURL(path), r)
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return <-errCh
}
