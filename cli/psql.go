package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("psql", runPsql, `
usage: flynn psql [-c <command>]

Open a console to a Flynn postgres database.

Options:
	-c, --command <command>  SQL command to run
`)
}

func runPsql(args *docopt.Args, client *controller.Client) error {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return fmt.Errorf("error getting app release: %s", err)
	}

	pgApp := appRelease.Env["FLYNN_POSTGRES"]
	if pgApp == "" {
		return fmt.Errorf("No postgres database found. Provision one with `flynn resource add postgres`")
	}

	pgRelease, err := client.GetAppRelease(pgApp)
	if err != nil {
		return fmt.Errorf("error getting postgres release: %s", err)
	}

	config := runConfig{
		App:        mustApp(),
		Release:    pgRelease.ID,
		Entrypoint: []string{"psql"},
		Env:        make(map[string]string, 4),
	}
	for _, k := range []string{"PGHOST", "PGUSER", "PGPASSWORD", "PGDATABASE"} {
		v := appRelease.Env[k]
		if v == "" {
			return fmt.Errorf("missing %s in app environment", k)
		}
		config.Env[k] = v
	}
	if cmd := args.String["--command"]; cmd != "" {
		config.Args = []string{"-c", cmd}
	}

	return runJob(client, config)
}
