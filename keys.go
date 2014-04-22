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
	"text/tabwriter"

	"github.com/flynn/flynn-controller/client"
)

var cmdKeys = &Command{
	Run:   runKeys,
	Usage: "keys",
	Short: "list ssh public keys",
	Long: `
Command keys lists SSH public keys associated with the Flynn controller.

Examples:

    $ flynn keys
    5e:67:40:b6:79:db:56:47:cd:3a:a7:65:ab:ed:12:34  user@test.com
`,
}

func runKeys(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 0 {
		cmd.printUsage(true)
	}

	keys, err := client.KeyList()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
	defer w.Flush()

	for i := range keys {
		listRec(w,
			formatKeyID(keys[i].ID),
			keys[i].Comment,
		)
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

var cmdKeyAdd = &Command{
	Run:   runKeyAdd,
	Usage: "key-add [<public-key-file>]",
	Short: "add ssh public key",
	Long: `
Command key-add adds an ssh public key to the Flynn controller.

It tries these sources for keys, in order:

1. public-key-file argument, if present
2. output of ssh-add -L, if any
3. file $HOME/.ssh/id_rsa.pub
`,
}

func runKeyAdd(cmd *Command, args []string, client *controller.Client) error {
	if len(args) > 1 {
		cmd.printUsage(true)
	}
	var sshPubKeyPath string
	if len(args) == 1 {
		sshPubKeyPath = args[0]
	}
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

var cmdKeyRemove = &Command{
	Run:   runKeyRemove,
	Usage: "key-remove <fingerprint>",
	Short: "remove an ssh public key",
	Long: `
Command key-remove removes an ssh public key from the Flynn controller.

Examples:

    $ flynn key-remove 5e:67:40:b6:79:db:56:47:cd:3a:a7:65:ab:ed:12:34
    Key 5e:67:40:b6:79:db… removed.
`,
}

func runKeyRemove(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}
	fingerprint := args[0]

	if err := client.DeleteKey(fingerprint); err != nil {
		return err
	}
	log.Printf("Key %s removed.", fingerprint)
	return nil
}
