package main

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
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

	register("info", runInfo, `
usage: flynn info

Show information for an app.

Examples:

	$ flynn info
	=== example
	Git URL:  https://git.dev.localflynn.com/example.git
	Web URL:  http://example.dev.localflynn.com

	$ flynn -a example info
	=== example
	Git URL:  https://git.dev.localflynn.com/example.git
	Web URL:  http://example.dev.localflynn.com
`)
}

func runCreate(args *docopt.Args, client controller.Client) error {
	app := &ct.App{}
	app.Name = args.String["<name>"]
	remote := args.String["--remote"]

	if inGitRepo() && !args.Bool["--yes"] {
		// Test if remote name exists and prompt user
		update, err := promptReplaceRemote(remote)
		if err != nil {
			return err
		}
		if update == false {
			return nil
		}
	}

	// Create the app
	if err := client.CreateApp(app); err != nil {
		return err
	}

	// Register git remote
	if inGitRepo() && remote != "" {
		exec.Command("git", "remote", "remove", remote).Run()
		exec.Command("git", "remote", "add", "--", remote, gitURL(clusterConf, app.Name)).Run()
	}
	log.Printf("Created %s", app.Name)
	return nil
}

func runDelete(args *docopt.Args, client controller.Client) error {
	appName := mustApp()
	remote := args.String["--remote"]

	if !args.Bool["--yes"] {
		if !promptYesNo(fmt.Sprintf("Are you sure you want to delete the app %q?", appName)) {
			return nil
		}
	}

	// scale formation down to 0
	if err := scaleToZero(appName, client); err != nil {
		return err
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

	log.Printf("Deleted %s (removed %d routes, deleted %d releases, deprovisioned %d resources)",
		appName, len(res.DeletedRoutes), len(res.DeletedReleases), len(res.DeletedResources))
	return nil
}

func runApps(args *docopt.Args, client controller.Client) error {
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

func runInfo(_ *docopt.Args, client controller.Client) error {
	appName := mustApp()

	fmt.Println("===", appName)

	w := tabWriter()
	defer w.Flush()

	if release, err := client.GetAppRelease(appName); err == nil || err == controller.ErrNotFound {
		if err == controller.ErrNotFound || release.IsGitDeploy() {
			listRec(w, "Git URL:", gitURL(clusterConf, appName))
		}
	} else {
		return err
	}

	if routes, err := client.RouteList(appName); err == nil {
		for _, k := range routes {
			if k.Type == "http" {
				route := k.HTTPRoute()
				protocol := "https"
				if route.Certificate == nil && route.LegacyTLSCert == "" {
					protocol = "http"
				}
				listRec(w, "Web URL:", protocol+"://"+route.Domain)
				break
			}
		}
	}

	return nil
}

func scaleToZero(appName string, client controller.Client) error {
	release, err := client.GetAppRelease(appName)
	if err == controller.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}
	opts := ct.ScaleOptions{Processes: make(map[string]int, len(release.Processes))}
	for typ := range release.Processes {
		opts.Processes[typ] = 0
	}
	return client.ScaleAppRelease(appName, release.ID, opts)
}
