package main

import (
	"log"
	"net"
	"net/url"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/flynn/flynn-controller/client"
)

var cmdServerAdd = &Command{
	Run:      runServerAdd,
	Usage:    "server-add [-g <githost>] [-p <tlspin>] <server-name> <url> <key>",
	Short:    "add a server",
	Long:     `Command add-server adds a server to the ~/.flynnrc configuration file`,
	NoClient: true,
}

var serverGitHost string
var serverTLSPin string

func init() {
	cmdServerAdd.Flag.StringVarP(&serverGitHost, "git-host", "g", "", "git host (if host differs from api URL host)")
	cmdServerAdd.Flag.StringVarP(&serverTLSPin, "tls-pin", "p", "", "SHA256 of the server's TLS cert (useful if it is self-signed)")
}

func runServerAdd(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 3 {
		cmd.printUsage(true)
	}
	if err := readConfig(); err != nil {
		return err
	}

	s := &ServerConfig{
		Name:    args[0],
		URL:     args[1],
		Key:     args[2],
		GitHost: serverGitHost,
		TLSPin:  serverTLSPin,
	}
	if serverGitHost == "" {
		u, err := url.Parse(s.URL)
		if err != nil {
			return err
		}
		if host, _, err := net.SplitHostPort(u.Host); err == nil {
			s.GitHost = host
		} else {
			s.GitHost = u.Host
		}
	}
	config.Servers = append(config.Servers, s)

	f, err := os.Create(configPath())
	if err != nil {
		return err
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(config); err != nil {
		return err
	}

	log.Printf("Server %s added.", s.Name)
	return nil
}
