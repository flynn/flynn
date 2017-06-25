package main

import (
	"log"
	"os/exec"

	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/go-docopt"
)

func init() {
	register("remote", runRemote, `
usage: flynn remote add [<remote>] [-y]

Create a git remote that allows deploying the application via git.
If a name for the remote is not provided 'flynn' will be used.

Note that the -a <app> option must be given so the remote to add is known.

Options:
	-y, --yes              Skip the confirmation prompt if the git remote already exists.

Examples:

	$ flynn -a turkeys-stupefy-perry remote add
	Created remote flynn with url https://git.dev.localflynn.com/turkeys-stupefy-perry.git

	$ flynn -a turkeys-stupefy-perry remote add staging
	Created remote staging with url https://git.dev.localflynn.com/turkeys-stupefy-perry.git
`)
}

func runRemote(args *docopt.Args, client controller.Client) error {
	app, err := client.GetApp(mustApp())
	if err != nil {
		return err
	}

	remote := args.String["<remote>"]
	if remote == "" {
		remote = "flynn"
	}

	if !inGitRepo() {
		log.Print("Must be executed within a git repository.")
		return nil
	}

	if !args.Bool["--yes"] {
		update, err := promptReplaceRemote(remote)
		if err != nil {
			return err
		}
		if update == false {
			return nil
		}
	}

	// Register git remote
	url := gitURL(clusterConf, app.Name)
	exec.Command("git", "remote", "remove", remote).Run()
	exec.Command("git", "remote", "add", "--", remote, url).Run()

	log.Printf("Created remote %s with url %s.", remote, url)
	return nil
}
