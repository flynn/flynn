package main

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/go-crypto-ssh"
	//"github.com/flynn/go-crypto-ssh/terminal"
	//"github.com/kr/pty"
)

type ServerTerminal struct {
	ssh.ServerTerminal
}

func (ss *ServerTerminal) ReadLine() (line string, err error) {
	for {
		if line, err = ss.Term.ReadLine(); err == nil {
			return
		}

		req, ok := err.(ssh.ChannelRequest)
		if !ok {
			return
		}

		ok = false
		log.Println(req.Request)
		log.Println(string(req.Payload[4:]))
		switch req.Request {
		/*case "pty-req":
		var width, height int
		width, height, ok = parsePtyRequest(req.Payload)
		ss.Term.SetSize(width, height)*/
		case "shell":
			ok = true
		case "env":
			ok = true
		case "exec":
			ss.Channel.AckRequest(true)
			err = nil
			line = string(req.Payload[4:])
			return
		}
		if req.WantReply {
			ss.Channel.AckRequest(ok)
		}
	}
	panic("unreachable")
}

func execCmd(cmdline string) (io.ReadCloser, io.ReadCloser, io.WriteCloser, chan uint) {
	// shitty cmdline tokenization
	cmdline = strings.Replace(cmdline, "'", "", -1)
	cmdargs := strings.Split(cmdline, " ")
	log.Println(cmdargs)
	cmd := exec.Command(cmdargs[0], cmdargs[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}
	err = cmd.Start()
	if err != nil {
		panic(err)
	}
	exitCh := make(chan uint, 1)
	go func() {
		log.Println("waiting...")
		err = cmd.Wait()
		if err != nil {
			log.Println(err)
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
		log.Println("exit 0")
		exitCh <- uint(0)
	}()
	return stdout, stderr, stdin, exitCh
}

func main() {
	// An SSH server is represented by a ServerConfig, which holds
	// certificate details and handles authentication of ServerConns.
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn *ssh.ServerConn, user, pass string) bool {
			return user == "testuser" && pass == "tiger"
		},
	}

	pemBytes, err := ioutil.ReadFile("id_rsa")
	if err != nil {
		log.Fatal("Failed to load private key:", err)
	}
	if err = config.SetRSAPrivateKey(pemBytes); err != nil {
		log.Fatal("Failed to parse private key:", err)
	}

	// Once a ServerConfig has been configured, connections can be
	// accepted.
	conn, err := ssh.Listen("tcp", "0.0.0.0:2022", config)
	if err != nil {
		log.Fatal("failed to listen for connection")
	}
	for {
		// A ServerConn multiplexes several channels, which must
		// themselves be Accepted.
		log.Println("accept")
		sConn, err := conn.Accept()
		if err != nil {
			log.Println("failed to accept incoming connection")
			continue
		}
		if err := sConn.Handshake(); err != nil {
			log.Println("failed to handshake")
			continue
		}
		go handleServerConn(sConn)
	}
}

func handleServerConn(sConn *ssh.ServerConn) {
	defer sConn.Close()
	for {
		// Accept reads from the connection, demultiplexes packets
		// to their corresponding channels and returns when a new
		// channel request is seen. Some goroutine must always be
		// calling Accept; otherwise no messages will be forwarded
		// to the channels.
		ch, err := sConn.Accept()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Println("handleServerConn Accept:", err)
			break
		}
		// Channels have a type, depending on the application level
		// protocol intended. In the case of a shell, the type is
		// "session" and ServerShell may be used to present a simple
		// terminal interface.
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			break
		}
		go handleChannel(ch)
	}
}

func handleChannel(ch ssh.Channel) {
	err := ch.Accept()
	if err != nil {
		panic(err)
	}
	defer ch.Close()
	for {
		log.Println("ready")
		req, err := ch.ReadRequest()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Println("handleChannel read request err:", err)
			continue
		}
		log.Println(req.Request)
		switch req.Request {
		case "exec":
			if req.WantReply {
				ch.AckRequest(true)
			}
			cmdline := string(req.Payload[4:])
			errCh := make(chan error)
			stdout, stderr, stdin, exitCh := execCmd(cmdline)
			go func() {
				_, e := io.Copy(os.Stdout, stderr)
				if e != nil {
					panic(e)
				}
			}()
			go func() {
				_, e := io.Copy(ch, io.TeeReader(stdout, os.Stdout))
				if e != nil {
					panic(e)
				}
				log.Println("program EOF")
				errCh <- e
			}()
			go func() {
				written, e := io.Copy(stdin, ch)
				if e != nil {
					panic(e)
				}
				log.Println("upload EOF", written)
				//stdin.Close()
				errCh <- e
			}()
			<-errCh
			<-errCh
			ch.Exit(<-exitCh)
			log.Println("done")
		case "env":
			if req.WantReply {
				ch.AckRequest(true)
			}
		}
	}
}
