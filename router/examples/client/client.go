package main

import (
	"bytes"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/flynn/flynn/router/client"
	"github.com/flynn/flynn/router/types"
)

func main() {
	certPath := flag.String("cert", "", "path to DER encoded certificate for SSL, - for stdin")
	keyPath := flag.String("key", "", "path to DER encoded private key for SSL, - for stdin")
	flag.Parse()
	if len(flag.Args()) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] domain service-name\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(64)
	}

	domain, serviceName := flag.Arg(0), flag.Arg(1)

	var stdin []byte
	var err error
	if *certPath == "-" || *keyPath == "-" {
		stdin, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatal("Failed to read from stdin: ", err)
		}
	}

	tlsCert, err := readCert(*certPath, stdin)
	if err != nil {
		return
	}
	tlsKey, err := readKey(*keyPath, stdin)
	if err != nil {
		return
	}

	client := client.New()

	conf := &router.HTTPRoute{
		Service: serviceName,
		Domain:  domain,
		Certificate: &router.Certificate{
			Cert: string(tlsCert),
			Key:  string(tlsKey),
		},
	}
	if err := client.CreateRoute(conf.ToRoute()); err != nil {
		log.Fatal(err)
	}
}

func readCert(path string, stdin []byte) ([]byte, error) {
	if path == "-" {
		var buffer bytes.Buffer
		var derBlock *pem.Block
		for {
			derBlock, stdin = pem.Decode(stdin)
			if derBlock == nil {
				break
			}
			if derBlock.Type == "CERTIFICATE" {
				buffer.Write(pem.EncodeToMemory(derBlock))
			}
		}
		if buffer.Len() > 0 {
			return buffer.Bytes(), nil
		}
		log.Fatal("No certificate PEM blocks found in stdin")
	}
	return readFile(path)
}

func readKey(path string, stdin []byte) ([]byte, error) {
	if path == "-" {
		var derBlock *pem.Block
		for {
			derBlock, stdin = pem.Decode(stdin)
			if derBlock == nil {
				break
			}
			if strings.Contains(derBlock.Type, "PRIVATE KEY") {
				return pem.EncodeToMemory(derBlock), nil
			}
		}
		log.Fatal("No private key PEM blocks found in stdin")
	}
	return readFile(path)
}

func readFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}

	contents, err := ioutil.ReadFile(path)
	if err != nil {
		log.Printf("Failed to open %s: %s", path, err)
		return nil, err
	}
	return contents, nil
}
