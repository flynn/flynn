package installer

import (
	"fmt"
	"net"
	"time"

	"src/golang.org/x/crypto/ssh"
)

type TargetServer interface {
	User() string
	IP() string
	Port() string
	SSHConfig() *ssh.ClientConfig
	SSHClient() (*ssh.Client, error)
}

func (c *Client) NewTargetServer(user, ip, port string, auth []ssh.AuthMethod) TargetServer {
	return &targetServer{
		user: user,
		ip:   ip,
		port: port,
		sshConfig: &ssh.ClientConfig{
			User: user,
			Auth: auth,
		},
		c: c,
	}
}

type targetServer struct {
	user      string
	ip        string
	port      string
	sshConfig *ssh.ClientConfig
	sshClient *ssh.Client
	c         *Client
}

func (t *targetServer) User() string {
	return t.user
}

func (t *targetServer) IP() string {
	return t.ip
}

func (t *targetServer) Port() string {
	return t.port
}

func (t *targetServer) SSHConfig() *ssh.ClientConfig {
	return t.sshConfig
}

func (t *targetServer) SSHClient() (*ssh.Client, error) {
	if t.sshClient != nil {
		return t.sshClient, nil
	}
	t.c.SendLogEvent(fmt.Sprintf("Establishing SSH connection to %s@%s:%s", t.user, t.ip, t.port))
	attempts := 0
	maxAttempts := 30
	for {
		var err error
		t.sshClient, err = ssh.Dial("tcp", net.JoinHostPort(t.ip, t.port), t.sshConfig)
		if err != nil {
			t.c.SendLogEvent(fmt.Sprintf("Error connecting to %s@%s:%s (attempt %02d/%d): %s", t.user, t.ip, t.port, attempts, maxAttempts, err))
			if attempts < maxAttempts {
				attempts++
				time.Sleep(time.Second)
				continue
			}
			return nil, err
		}
		break
	}
	t.c.SendLogEvent(fmt.Sprintf("Successfully established SSH connection to %s@%s:%s", t.user, t.ip, t.port))
	return t.sshClient, nil
}
