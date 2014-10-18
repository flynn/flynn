package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("key", runKey, `
usage: flynn key
       flynn key add [<public-key-file>]
       flynn key remove <fingerprint>

Manage SSH public keys associated with the Flynn controller.

Commands:
	With no arguments, shows a list of SSH public keys.

	add     adds an ssh public key to the Flynn controller

		It tries these sources for keys, in order:

		1. <public-key-file> argument
		2. output of ssh-add -L, if any
		3. file $HOME/.ssh/id_rsa.pub

	remove  removes an ssh public key from the Flynn controller.

Examples:

	$ flynn key
	5e:67:40:b6:79:db:56:47:cd:3a:a7:65:ab:ed:12:34  user@test.com

	$ flynn key remove 5e:67:40:b6:79:db:56:47:cd:3a:a7:65:ab:ed:12:34
	Key 5e:67:40:b6:79:db… removed.
`)
}

func runKey(args *docopt.Args, client *controller.Client) error {
	if args.Bool["add"] {
		return runKeyAdd(args, client)
	} else if args.Bool["remove"] {
		return runKeyRemove(args, client)
	}

	keys, err := client.KeyList()
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	for _, k := range keys {
		listRec(w, formatKeyID(k.ID), k.Comment)
	}
	return nil
}

func formatKeyID(s string) string {
	buf := make([]byte, 0, len(s)+((len(s)-2)/2))
	for i := range s {
		buf = append(buf, s[i])
		if (i+1)%2 == 0 && i != len(s)-1 {
			buf = append(buf, ':')
		}
	}
	return string(buf)
}

func runKeyAdd(args *docopt.Args, client *controller.Client) error {
	sshPubKeyPath := args.String["<public-key-file>"]

	keys, err := findKeys(sshPubKeyPath)
	if err != nil {
		if _, ok := err.(privKeyError); ok {
			log.Println("refusing to upload")
		}
		return err
	}

	key, err := client.CreateKey(string(keys))
	if err != nil {
		return err
	}
	log.Printf("Key %s added.", formatKeyID(key.ID))
	return nil
}

func findKeys(sshPubKeyPath string) ([]byte, error) {
	if sshPubKeyPath != "" {
		return sshReadPubKey(sshPubKeyPath)
	}

	out, err := exec.Command("ssh-add", "-L").Output()
	if err == nil && len(out) != 0 {
		return out, nil
	}

	var key []byte
	for _, f := range []string{"id_rsa.pub", "id_dsa.pub"} {
		key, err = sshReadPubKey(filepath.Join(homedir(), ".ssh", f))
		if err == nil {
			return key, nil
		}
	}
	if err == syscall.ENOENT {
		err = errors.New("No SSH keys found")
	}
	return nil, err
}

func sshReadPubKey(s string) ([]byte, error) {
	f, err := os.Open(filepath.FromSlash(s))
	if err != nil {
		return nil, err
	}

	key, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	if bytes.Contains(key, []byte("PRIVATE")) {
		return nil, privKeyError(s)
	}

	return key, nil
}

type privKeyError string

func (e privKeyError) Error() string {
	return "appears to be a private key: " + string(e)
}

func runKeyRemove(args *docopt.Args, client *controller.Client) error {
	fingerprint := args.String["<fingerprint>"]

	if err := client.DeleteKey(fingerprint); err != nil {
		return err
	}
	log.Printf("Key %s removed.", fingerprint)
	return nil
}
