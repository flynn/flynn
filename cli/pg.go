package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("pg", runPg, `
usage: flynn pg psql [--] [<argument>...]
       flynn pg dump [-q] [-f <file>]
       flynn pg restore [-q] [-j <jobs>] [-f <file>]

Options:
	-f, --file=<file>  name of dump file
	-q, --quiet        don't print progress
	-j, --jobs=<jobs>  number of pg_restore jobs to use [default: 1]

Commands:
	psql     Open a console to a Flynn postgres database. Any valid arguments to psql may be provided.
	dump     Dump a postgres database. If file is not specified, will dump to stdout.
	restore  Restore a database dump. If file is not specified, will restore from stdin.

Examples:

    $ flynn pg psql

    $ flynn pg psql -- -c "CREATE EXTENSION hstore"

    $ flynn pg dump -f db.dump

    $ flynn pg restore -j 8 -f db.dump
`)
}

func runPg(args *docopt.Args, client controller.Client) error {
	config, err := getAppPgRunConfig(client)
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

func getAppPgRunConfig(client controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}
	return getPgRunConfig(client, mustApp(), appRelease)
}

func getPgRunConfig(client controller.Client, app string, appRelease *ct.Release) (*runConfig, error) {
	pgApp := appRelease.Env["FLYNN_POSTGRES"]
	if pgApp == "" {
		return nil, fmt.Errorf("No postgres database found. Provision one with `flynn resource add postgres`")
	}

	pgRelease, err := client.GetAppRelease(pgApp)
	if err != nil {
		return nil, fmt.Errorf("error getting postgres release: %s", err)
	}

	config := &runConfig{
		App:        app,
		Release:    pgRelease.ID,
		Env:        make(map[string]string),
		DisableLog: true,
		Exit:       true,
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

func runPsql(args *docopt.Args, client controller.Client, config *runConfig) error {
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = append([]string{"psql"}, args.All["<argument>"].([]string)...)
	return runJob(client, *config)
}

func runPgDump(args *docopt.Args, client controller.Client, config *runConfig) error {
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
		bar.ShowBar = false
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
		config.Stdout = io.MultiWriter(config.Stdout, bar)
	}
	return pgDump(client, config)
}

func configPgDump(config *runConfig) {
	config.Args = []string{"pg_dump", "--format=custom", "--no-owner", "--no-acl"}
}

func pgDump(client controller.Client, config *runConfig) error {
	configPgDump(config)
	return runJob(client, *config)
}

func runPgRestore(args *docopt.Args, client controller.Client, config *runConfig) error {
	jobs, err := strconv.Atoi(args.String["--jobs"])
	if err != nil {
		return err
	}
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
		bar := pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		if size > 0 {
			bar.Total = size
		} else {
			bar.ShowBar = false
		}
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		defer bar.Finish()
		config.Stdin = bar.NewProxyReader(config.Stdin)
	}

	return pgRestore(client, config, jobs)
}

func pgRestore(client controller.Client, config *runConfig, jobs int) error {
	if jobs > 1 {
		// Check if controller supports data volumes for one-off jobs
		minVersion := "v20161018.0"
		compatible, err := compatCheck(client, minVersion)
		if err != nil {
			return err
		} else if !compatible {
			fmt.Fprintf(os.Stderr, "WARN: cluster versions prior to %s don't support the --jobs argument for parallel restore, falling back to --jobs=1\n", minVersion)
			jobs = 1
		}
	}
	if jobs > 1 {
		// Provision a volume at /data to stream the dump to.
		config.Data = true
		config.Args = []string{
			"bash",
			"-c",
			fmt.Sprintf("set -o pipefail; cat > /data/temp.dump && pg_restore -d %s -n public --clean --if-exists --no-owner --no-acl --jobs %d /data/temp.dump", config.Env["PGDATABASE"], jobs),
		}
	} else {
		config.Args = []string{"pg_restore", "-d", config.Env["PGDATABASE"], "-n", "public", "--clean", "--if-exists", "--no-owner", "--no-acl"}
	}
	err := runJob(client, *config)
	if exit, ok := err.(RunExitError); ok && exit == 1 {
		// pg_restore exits with zero if there are warnings
		return nil
	}
	return err
}
