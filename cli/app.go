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
usage: flynn create [<name>]

Create an application in Flynn.
`)

	register("delete", runDelete, `
usage: flynn delete

Delete Flynn app.
`)
	register("apps", runApps, `
usage: flynn apps

List flynn apps.
`)
}

func runCreate(args *docopt.Args, client *controller.Client) error {
	app := &ct.App{}
	app.Name = args.String["<name>"]

	if err := client.CreateApp(app); err != nil {
		return err
	}

	exec.Command("git", "remote", "remove", "flynn").Run()
	exec.Command("git", "remote", "add", "flynn", gitURLPre(clusterConf.GitHost)+app.Name+gitURLSuf).Run()
	log.Printf("Created %s", app.Name)
	return nil
}

func runDelete(args *docopt.Args, client *controller.Client) error {
	appName := mustApp()

	fmt.Printf("Are you sure you want to delete the app %q? (yes/no): ", appName)
loop:
	for {
		var answer string
		fmt.Scanln(&answer)
		switch answer {
		case "y", "yes":
			break loop
		case "n", "no":
			return nil
		default:
			fmt.Print("Please type 'yes' or 'no': ")
		}
	}

	if err := client.DeleteApp(appName); err != nil {
		return err
	}

	if remotes, err := gitRemotes(); err == nil {
		if app, ok := remotes["flynn"]; ok && app.Name == appName {
			exec.Command("git", "remote", "remove", "flynn").Run()
		}
	}

	log.Printf("Deleted %s", appName)
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
