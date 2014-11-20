package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/flynn/flynn/pkg/certgen"
)

func writeCert(externalIP, dir string) error {
	fmt.Println("EXTERNAL_IP is", net.ParseIP(externalIP))

	cert, err := certgen.Generate(certgen.Params{Hosts: []string{externalIP}})
	if err != nil {
		return err
	}

	certOut, err := os.Create(filepath.Join(dir, "server.crt"))
	if err != nil {
		return err
	}
	certOut.Write([]byte(cert.PEM))
	certOut.Close()

	keyOut, err := os.OpenFile(filepath.Join(dir, "server.key"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	keyOut.Write([]byte(cert.KeyPEM))
	keyOut.Close()

	return nil
}
