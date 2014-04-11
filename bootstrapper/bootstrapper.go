package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/flynn/flynn-bootstrap"
)

func manifest() ([]byte, error) {
	if len(os.Args) == 1 || os.Args[1] == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ioutil.ReadAll(f)
}

func main() {
	manifest, err := manifest()
	if err != nil {
		log.Fatalln("Error reading manifest:", err)
	}

	if err := bootstrap.Run(manifest); err != nil {
		log.Fatal(err)
	}
}
