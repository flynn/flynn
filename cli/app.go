package main

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("create", runCreate, `
usage: flynn create [-r <remote>] [-y] [<name>]

Create an application in Flynn.

If a name is not provided, a random name will be generated.

If run from a git repository, a 'flynn' remote will be created or replaced that
allows deploying the application via git.

Options:
	-r, --remote=<remote>  Name of git remote to create, empty string for none. [default: flynn]
	-y, --yes              Skip the confirmation prompt if the git remote already exists.

Examples:

	$ flynn create
	Created turkeys-stupefy-perry
`)

	register("delete", runDelete, `
usage: flynn delete [-y] [-r <remote>]

Delete an app.

If run from a git repository with a 'flynn' remote for the app, it will be
removed.

Options:
	-r, --remote=<remote>  Name of git remote to delete, empty string for none. [default: flynn]
	-y, --yes              Skip the confirmation prompt.

Examples:

	$ flynn -a turkeys-stupefy-perry delete
	Are you sure you want to delete the app "turkeys-stupefy-perry"? (yes/no): yes
	Deleted turkeys-stupefy-perry
`)
	register("apps", runApps, `
usage: flynn apps

List all apps.

Examples:

	$ flynn apps
	ID                                NAME
	f1e85f5392454a329929e3f27f7a5644  gitreceive
	4c6325c1f13547059e5496c91a6a97dd  router
	8cfd94d040b14bd8aecc086c8f5f5e0d  blobstore
	f488cfb478f54edea497bf6347c2eb80  postgres
	9d5be7be873c41b9898032c08aa87597  controller
`)
}

func runCreate(args *docopt.Args, client *controller.Client) error {
	app := &ct.App{}
	app.Name = args.String["<name>"]
	remote := args.String["--remote"]

	if inGitRepo() && !args.Bool["--yes"] {
		// Test if remote name exists and prompt user
		remotes, err := gitRemoteNames()
		if err != nil {
			return err
		}

		for _, r := range remotes {
			if r == remote {
				fmt.Println("There is already a git remote called", remote)
				if !promptYesNo("Are you sure you want to replace it?") {
					log.Println("The app was not created. Please, declare the desired local git remote name with --remote flag.")
					return nil
				}
			}
		}
	}

	// Create the app
	if err := client.CreateApp(app); err != nil {
		return err
	}

	// Register git remote
	if inGitRepo() && remote != "" {
		exec.Command("git", "remote", "remove", remote).Run()
		exec.Command("git", "remote", "add", "--", remote, gitURLPre(clusterConf.GitHost)+app.Name+gitURLSuf).Run()
	}
	log.Printf("Created %s", app.Name)
	return nil
}

func runDelete(args *docopt.Args, client *controller.Client) error {
	appName := mustApp()
	remote := args.String["--remote"]

	if !args.Bool["--yes"] {
		if !promptYesNo(fmt.Sprintf("Are you sure you want to delete the app %q?", appName)) {
			return nil
		}
	}

	res, err := client.DeleteApp(appName)
	if err != nil {
		return err
	}

	if remote != "" {
		if remotes, err := gitRemotes(); err == nil {
			if app, ok := remotes[remote]; ok && app.Name == appName {
				exec.Command("git", "remote", "remove", remote).Run()
			}
		}
	}

	log.Printf("Deleted %s (removed %d routes, deprovisioned %d resources)",
		appName, len(res.DeletedRoutes), len(res.DeletedResources))
	return nil
}

func runApps(args *docopt.Args, client *controller.Client) error {
	apps, err := client.AppList()
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "NAME")
	for _, a := range apps {
		listRec(w, a.ID, a.Name)
	}
	return nil
}
