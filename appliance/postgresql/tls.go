package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/flynn/flynn/pkg/certgen"
)

func writeCert(externalIP, dir string) error {
	fmt.Println("EXTERNAL_IP is", net.ParseIP(externalIP))

	certOptions := certgen.Certificate{
		Lifespan: 5 * 365 * 24 * time.Hour,
		Hosts:    []string{externalIP},
	}
	cert, privKey, _, err := certgen.Generate(certOptions)
	if err != nil {
		return err
	}

	certOut, err := os.Create(filepath.Join(dir, "server.crt"))
	if err != nil {
		return err
	}
	certOut.Write([]byte(cert))
	certOut.Close()

	keyOut, err := os.OpenFile(filepath.Join(dir, "server.key"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	keyOut.Write([]byte(privKey))
	keyOut.Close()

	return nil
}
