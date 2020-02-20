package main

import (
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/flynn/flynn/controller/api"
	controller "github.com/flynn/flynn/controller/client"
	"github.com/flynn/go-docopt"
	"github.com/golang/protobuf/ptypes"
	"github.com/olekukonko/tablewriter"
)

func init() {
	register("tls-key", runTLSKey, `
usage: flynn tls-key
       flynn tls-key add <key>
       flynn tls-key remove <name>

Manage TLS keys.

Commands:
	With no arguments, shows a list of keys.

	add     adds a key
	remove  removes a key

Arguments:
	<key>   path to PEM encoded private key, - for stdin
	<name>  name of the key to remove
`)
}

func runTLSKey(args *docopt.Args, client controller.Client) error {
	if args.Bool["add"] {
		return runTLSKeyAdd(args, client)
	} else if args.Bool["remove"] {
		return runTLSKeyRemove(args, client)
	}

	var req api.ListKeysRequest
	var res api.ListKeysResponse
	if err := client.Invoke("flynn.api.v1.Router/ListKeys", &req, &res); err != nil {
		return err
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowLine(true)
	table.SetAutoWrapText(false)
	table.SetHeader([]string{"NAME", "ALGORITHM", "CERTIFICATES", "CREATED"})
	defer table.Render()
	for _, key := range res.Keys {
		table.Append([]string{
			key.Name,
			key.Algorithm.String(),
			strings.Join(key.Certificates, "\n"),
			ptypes.TimestampString(key.CreateTime),
		})
	}

	return nil
}

func runTLSKeyAdd(args *docopt.Args, client controller.Client) error {
	// read the private key
	keyFile := args.String["<key>"]
	var in io.Reader
	if keyFile == "-" {
		in = os.Stdin
	} else {
		f, err := os.Open(keyFile)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	data, err := ioutil.ReadAll(in)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("failed to find any PEM data in key input")
	}

	// create the key
	req := api.CreateKeyRequest{
		PrivateKey: block.Bytes,
	}
	var res api.CreateKeyResponse
	if err := client.Invoke("flynn.api.v1.Router/CreateKey", &req, &res); err != nil {
		return err
	}
	fmt.Println(res.Key.Name)
	return nil
}

func runTLSKeyRemove(args *docopt.Args, client controller.Client) error {
	req := api.DeleteKeyRequest{
		Name: args.String["<name>"],
	}
	var res api.DeleteKeyResponse
	if err := client.Invoke("flynn.api.v1.Router/DeleteKey", &req, &res); err != nil {
		return err
	}
	fmt.Printf("Key %s removed.\n", res.Key.Name)
	return nil
}
