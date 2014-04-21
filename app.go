package main

import (
	"log"
	"os/exec"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdCreate = &Command{
	Run:   runCreate,
	Usage: "create [<name>]",
	Short: "create an app",
	Long:  `Create an application in Flynn`,
}

func runCreate(cmd *Command, args []string, client *controller.Client) error {
	if len(args) > 1 {
		cmd.printUsage(true)
	}

	app := &ct.App{}
	if len(args) > 0 {
		app.Name = args[0]
	}

	if err := client.CreateApp(app); err != nil {
		return err
	}

	exec.Command("git", "remote", "add", "flynn", gitURLPre(serverConf.GitHost)+app.Name+gitURLSuf).Run()
	log.Printf("Created %s", app.Name)
	return nil
}
