package main

import (
	"fmt"
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cheggaaa/pb"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("pg", runPg, `
usage: flynn pg psql [--] [<argument>...]
       flynn pg dump [-q] [-f <file>]
       flynn pg restore [-q] [-f <file>]

Options:
	-f, --file <file>  name of dump file
	-q, --quiet        don't print progress

Commands:
	psql     Open a console to a Flynn postgres database. Any valid arguments to psql may be provided.
	dump     Dump a postgres database. If file is not specified, will dump to stdout.
	restore  Restore a database dump. If file is not specified, will restore from stdin.

Examples:

    $ flynn pg psql

    $ flynn pg psql -- -c "CREATE EXTENSION hstore"

    $ flynn pg dump -f db.dump

    $ flynn pg restore -f db.dump
`)
}

func runPg(args *docopt.Args, client *controller.Client) error {
	config, err := getPgRunConfig(client)
	if err != nil {
		return err
	}
	switch {
	case args.Bool["psql"]:
		return runPsql(args, client, config)
	case args.Bool["dump"]:
		return runPgDump(args, client, config)
	case args.Bool["restore"]:
		return runPgRestore(args, client, config)
	}
	return nil
}

func getPgRunConfig(client *controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}

	pgApp := appRelease.Env["FLYNN_POSTGRES"]
	if pgApp == "" {
		return nil, fmt.Errorf("No postgres database found. Provision one with `flynn resource add postgres`")
	}

	pgRelease, err := client.GetAppRelease(pgApp)
	if err != nil {
		return nil, fmt.Errorf("error getting postgres release: %s", err)
	}

	config := &runConfig{
		App:        mustApp(),
		Release:    pgRelease.ID,
		Env:        make(map[string]string),
		DisableLog: true,
	}
	for _, k := range []string{"PGHOST", "PGUSER", "PGPASSWORD", "PGDATABASE"} {
		v := appRelease.Env[k]
		if v == "" {
			return nil, fmt.Errorf("missing %s in app environment", k)
		}
		config.Env[k] = v
	}
	return config, nil
}

func runPsql(args *docopt.Args, client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"psql"}
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = args.All["<argument>"].([]string)
	return runJob(client, *config)
}

func runPgDump(args *docopt.Args, client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"pg_dump"}
	config.Args = []string{"--format=custom", "--no-owner", "--no-acl"}
	config.Stdout = os.Stdout
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer f.Close()
		config.Stdout = f
	}
	if !args.Bool["--quiet"] && term.IsTerminal(os.Stderr.Fd()) {
		bar := pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
		config.Stdout = io.MultiWriter(config.Stdout, bar)
	}
	return runJob(client, *config)
}

func runPgRestore(args *docopt.Args, client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"pg_restore"}
	config.Args = []string{"-d", config.Env["PGDATABASE"], "--clean", "--no-owner", "--no-acl"}
	config.Stdin = os.Stdin
	var size int64
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			return err
		}
		size = stat.Size()
		config.Stdin = f
	}
	if !args.Bool["--quiet"] && term.IsTerminal(os.Stderr.Fd()) {
		bar := pb.New64(size)
		bar.SetUnits(pb.U_BYTES)
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
		config.Stdin = bar.NewProxyReader(config.Stdin)
	}
	return runJob(client, *config)
}
