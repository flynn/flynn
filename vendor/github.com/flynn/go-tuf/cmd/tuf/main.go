package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/pkg/term"
	"github.com/flynn/go-docopt"
	"github.com/flynn/go-tuf"
	"github.com/flynn/go-tuf/util"
)

func main() {
	log.SetFlags(0)

	usage := `usage: tuf [-h|--help] [-d|--dir=<dir>] [--insecure-plaintext] <command> [<args>...]

Options:
  -h, --help
  -d <dir>              The path to the repository (defaults to the current working directory)
  --insecure-plaintext  Don't encrypt signing keys

Commands:
  help         Show usage for a specific command
  gen-key      Generate a new signing key for a specific manifest
  revoke-key   Revoke a signing key
  add          Add target file(s)
  remove       Remove a target file
  snapshot     Update the snapshot manifest
  timestamp    Update the timestamp manifest
  sign         Sign a manifest
  commit       Commit staged files to the repository
  regenerate   Recreate the targets manifest
  clean        Remove all staged manifests
  root-keys    Output a JSON serialized array of root keys to STDOUT

See "tuf help <command>" for more information on a specific command
`

	args, _ := docopt.Parse(usage, nil, true, "", true)
	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if cmd == "help" {
		if len(cmdArgs) == 0 { // `tuf help`
			fmt.Println(usage)
			return
		} else { // `tuf help <command>`
			cmd = cmdArgs[0]
			cmdArgs = []string{"--help"}
		}
	}

	dir, ok := args.String["-d"]
	if !ok {
		dir = args.String["--dir"]
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := runCommand(cmd, cmdArgs, dir, args.Bool["--insecure-plaintext"]); err != nil {
		log.Fatalln("ERROR:", err)
	}
}

type cmdFunc func(*docopt.Args, *tuf.Repo) error

type command struct {
	usage string
	f     cmdFunc
}

var commands = make(map[string]*command)

func register(name string, f cmdFunc, usage string) {
	commands[name] = &command{usage: usage, f: f}
}

func runCommand(name string, args []string, dir string, insecure bool) error {
	argv := make([]string, 1, 1+len(args))
	argv[0] = name
	argv = append(argv, args...)

	cmd, ok := commands[name]
	if !ok {
		return fmt.Errorf("%s is not a tuf command. See 'tuf help'", name)
	}

	parsedArgs, err := docopt.Parse(cmd.usage, argv, true, "", true)
	if err != nil {
		return err
	}

	var p util.PassphraseFunc
	if !insecure {
		p = getPassphrase
	}
	repo, err := tuf.NewRepo(tuf.FileSystemStore(dir, p))
	if err != nil {
		return err
	}
	return cmd.f(parsedArgs, repo)
}

func parseExpires(arg string) (time.Time, error) {
	days, err := strconv.Atoi(arg)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse --expires arg: %s", err)
	}
	return time.Now().AddDate(0, 0, days).UTC(), nil
}

func getPassphrase(role string, confirm bool) ([]byte, error) {
	if pass := os.Getenv(fmt.Sprintf("TUF_%s_PASSPHRASE", strings.ToUpper(role))); pass != "" {
		return []byte(pass), nil
	}

	state, err := term.SaveState(0)
	if err != nil {
		return nil, err
	}
	term.DisableEcho(0, state)
	defer term.RestoreTerminal(0, state)

	stdin := bufio.NewReader(os.Stdin)

	fmt.Printf("Enter %s keys passphrase: ", role)
	passphrase, err := stdin.ReadBytes('\n')
	fmt.Println()
	if err != nil {
		return nil, err
	}
	passphrase = passphrase[0 : len(passphrase)-1]

	if !confirm {
		return passphrase, nil
	}

	fmt.Printf("Repeat %s keys passphrase: ", role)
	confirmation, err := stdin.ReadBytes('\n')
	fmt.Println()
	if err != nil {
		return nil, err
	}
	confirmation = confirmation[0 : len(confirmation)-1]

	if !bytes.Equal(passphrase, confirmation) {
		return nil, errors.New("The entered passphrases do not match")
	}
	return passphrase, nil
}
