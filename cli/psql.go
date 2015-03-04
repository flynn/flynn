package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("psql", runPsql, `
usage: flynn psql [--] [<argument>...]

Open a console to a Flynn postgres database. Any valid arguments to psql may be
provided.

Examples:

    $ flynn psql

    $ flynn psql -- -c "CREATE EXTENSION hstore"
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
		Env: map[string]string{
			"PAGER": "less",
			"LESS":  "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof",
		},
		Args: args.All["<argument>"].([]string),
	}
	for _, k := range []string{"PGHOST", "PGUSER", "PGPASSWORD", "PGDATABASE"} {
		v := appRelease.Env[k]
		if v == "" {
			return fmt.Errorf("missing %s in app environment", k)
		}
		config.Env[k] = v
	}

	return runJob(client, config)
}
