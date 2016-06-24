package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

func init() {
	log.SetFlags(0)
}

const DestPath = "/tmp/app"

func parsePairs(args *docopt.Args, str string) (map[string]string, error) {
	pairs := args.All[str].([]string)
	item := make(map[string]string, len(pairs))
	for _, s := range pairs {
		v := strings.SplitN(s, "=", 2)
		if len(v) != 2 {
			return nil, fmt.Errorf("invalid var format: %q", s)
		}
		item[v[0]] = v[1]
	}
	return item, nil
}

func ensureDir(dir string) {
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0700); err != nil {
		log.Fatal(err)
	}
}

func main() {
	usage := `
Usage: taffy <app> <repo> <branch> <rev> [-e <var>=<val>]... [-m <key>=<val>]...

Options:
	-e,--env <var>=<val>
	-m,--meta <key>=<val>
`[1:]
	args, _ := docopt.Parse(usage, nil, true, version.String(), false)

	app := args.String["<app>"]
	repo := args.String["<repo>"]
	branch := args.String["<branch>"]
	rev := args.String["<rev>"]

	clientKey := os.Getenv("SSH_CLIENT_KEY")
	clientHosts := os.Getenv("SSH_CLIENT_HOSTS")
	homeFolder := os.Getenv("HOME")

	meta := map[string]string{
		"git":       "true",
		"clone_url": repo,
		"branch":    branch,
		"rev":       rev,
		"taffy_job": os.Getenv("FLYNN_JOB_ID"),
	}

	if homeFolder == "" || homeFolder == "/" {
		homeFolder = "/root"
	}

	if clientKey != "" {
		ensureDir(homeFolder)
		if err := ioutil.WriteFile(filepath.Join(homeFolder, ".ssh", "id_rsa"), []byte(clientKey), 0600); err != nil {
			log.Fatal(err)
		}
	}

	if clientHosts != "" {
		ensureDir(homeFolder)
		if err := ioutil.WriteFile(filepath.Join(homeFolder, ".ssh", "known_hosts"), []byte(clientHosts), 0600); err != nil {
			log.Fatal(err)
		}
	}

	env, err := parsePairs(args, "--env")
	if err != nil {
		log.Fatal(err)
	}
	m, err := parsePairs(args, "--meta")
	if err != nil {
		log.Fatal(err)
	}
	for k, v := range m {
		meta[k] = v
	}

	if err := cloneRepo(repo, branch); err != nil {
		log.Fatal(err)
	}
	if err := runReceiver(app, rev, env, meta); err != nil {
		log.Fatal(err)
	}
}

func cloneRepo(repo, branch string) error {
	cmd := exec.Command("git", "clone", "--depth=50", fmt.Sprintf("--branch=%s", branch), repo, DestPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitArchive(rev string, w io.Writer) error {
	cmd := exec.Command("git", "-C", DestPath, "archive", rev)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runReceiver(app, rev string, env, meta map[string]string) error {
	args := make([]string, 0, len(env)+len(meta)+2)
	args = append(args, app)
	args = append(args, rev)
	for name, m := range map[string]map[string]string{"--env": env, "--meta": meta} {
		for k, v := range m {
			args = append(args, name)
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}
	r, w := io.Pipe()

	cmd := exec.Command("/bin/flynn-receiver", args...)
	cmd.Stdin = r
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	errChan := make(chan error)
	go func() {
		defer w.Close()
		errChan <- gitArchive(rev, w)
	}()
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := <-errChan; err != nil {
		return err
	}
	return cmd.Wait()
}
