package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

var (
	apiURL = "http://localhost:1200"
	stdin  = bufio.NewReader(os.Stdin)
)

type Command struct {
	// args does not include the command name
	Run  func(cmd *Command, args []string)
	Flag flag.FlagSet

	Usage string // first word is the command name
	Short string // `hk help` output
	Long  string // `hk help cmd` output
}

func (c *Command) printUsage() {
	if c.Runnable() {
		fmt.Printf("Usage: flynn %s\n\n", c.Usage)
	}
	fmt.Println(strings.Trim(c.Long, "\n"))
}

func (c *Command) Name() string {
	name := c.Usage
	i := strings.Index(name, " ")
	if i >= 0 {
		name = name[:i]
	}
	return name
}

func (c *Command) Runnable() bool {
	return c.Run != nil
}

const extra = " (extra)"

func (c *Command) List() bool {
	return c.Short != "" && !strings.HasSuffix(c.Short, extra)
}

func (c *Command) ListAsExtra() bool {
	return c.Short != "" && strings.HasSuffix(c.Short, extra)
}

func (c *Command) ShortExtra() string {
	return c.Short[:len(c.Short)-len(extra)]
}

// Running `flynn help` will list commands in this order.
var commands = []*Command{
	cmdRun,
	cmdPs,
	cmdLogs,
	cmdScale,
	cmdDomain,
}

var (
	flagApp  string
	flagLong bool
)

func main() {
	if s := os.Getenv("FLYNN_API_URL"); s != "" {
		apiURL = strings.TrimRight(s, "/")
	}
	log.SetFlags(0)

	args := os.Args[1:]

	if len(args) >= 2 && "-a" == args[0] {
		flagApp = args[1]
		args = args[2:]

		if gitRemoteApp, err := appFromGitRemote(flagApp); err == nil {
			flagApp = gitRemoteApp
		}
	}

	if len(args) < 1 {
		usage()
	}

	for _, cmd := range commands {
		if cmd.Name() == args[0] && cmd.Run != nil {
			cmd.Flag.Usage = func() {
				cmd.printUsage()
			}
			if err := cmd.Flag.Parse(args[1:]); err != nil {
				os.Exit(2)
			}
			cmd.Run(cmd, cmd.Flag.Args())
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
	usage()
}

func app() (string, error) {
	if flagApp != "" {
		return flagApp, nil
	}

	if app := os.Getenv("FLYNN_APP"); app != "" {
		return app, nil
	}

	gitRemoteApp, err := appFromGitRemote("flynn")
	if err != nil {
		return "", err
	}

	return gitRemoteApp, nil
}

func appFromGitRemote(remote string) (string, error) {
	b, err := exec.Command("git", "config", "remote."+remote+".url").Output()
	if err != nil {
		if isNotFound(err) {
			wdir, _ := os.Getwd()
			return "", fmt.Errorf("could not find git remote "+remote+" in %s", wdir)
		}
		return "", err
	}

	out := strings.Trim(string(b), "\r\n ")

	if !strings.HasPrefix(out, gitURLPre) {
		return "", fmt.Errorf("could not find app name in " + remote + " git remote")
	}

	return out[len(gitURLPre):], nil
}

func isNotFound(err error) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.ProcessState.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus() == 1
		}
	}
	return false
}

func mustApp() string {
	name, err := app()
	if err != nil {
		log.Fatal(err)
	}
	return name
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func listRec(w io.Writer, a ...interface{}) {
	for i, x := range a {
		fmt.Fprint(w, x)
		if i+1 < len(a) {
			w.Write([]byte{'\t'})
		} else {
			w.Write([]byte{'\n'})
		}
	}
}
