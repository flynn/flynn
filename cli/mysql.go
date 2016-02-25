package main

import (
	"fmt"
	"io"
	"os"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cheggaaa/pb"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("mysql", runMysql, `
usage: flynn mysql console [--] [<argument>...]
       flynn mysql dump [-q] [-f <file>]
       flynn mysql restore [-q] [-f <file>]

Options:
	-f, --file=<file>  name of dump file
	-q, --quiet        don't print progress

Commands:
	console  Open a console to a Flynn mysql database. Any valid arguments to mysql may be provided.
	dump     Dump a mysql database. If file is not specified, will dump to stdout.
	restore  Restore a database dump. If file is not specified, will restore from stdin.

Examples:

    $ flynn mysql console

    $ flynn mysql console -- -e "CREATE DATABASE db"

    $ flynn mysql dump -f db.dump

    $ flynn mysql restore -f db.dump
`)
}

func runMysql(args *docopt.Args, client *controller.Client) error {
	config, err := getAppMysqlRunConfig(client)
	if err != nil {
		return err
	}

	switch {
	case args.Bool["console"]:
		return runMysqlMysql(args, client, config)
	case args.Bool["dump"]:
		return runMysqlDump(args, client, config)
	case args.Bool["restore"]:
		return runMysqlRestore(args, client, config)
	}
	return nil
}

func getAppMysqlRunConfig(client *controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}
	return getMysqlRunConfig(client, mustApp(), appRelease)
}

func getMysqlRunConfig(client *controller.Client, appName string, appRelease *ct.Release) (*runConfig, error) {
	app := appRelease.Env["FLYNN_MYSQL"]
	if app == "" {
		return nil, fmt.Errorf("No mysql database found. Provision one with `flynn resource add mysql`")
	}

	release, err := client.GetAppRelease(app)
	if err != nil {
		return nil, fmt.Errorf("error getting mysql release: %s", err)
	}

	if appRelease.Env["MYSQL_USER"] == "" {
		return nil, fmt.Errorf("missing MYSQL_USER in app environment")
	}

	config := &runConfig{
		App:        appName,
		Release:    release.ID,
		Env:        make(map[string]string),
		DisableLog: false, // true,
		Exit:       true,
	}

	for _, k := range []string{"MYSQL_HOST", "MYSQL_USER", "MYSQL_PWD", "MYSQL_DATABASE"} {
		v := appRelease.Env[k]
		if v == "" {
			return nil, fmt.Errorf("missing %s in app environment", k)
		}
		config.Env[k] = v
	}
	return config, nil
}

func runMysqlMysql(args *docopt.Args, client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"mysql"}
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = append([]string{
		"-u", config.Env["MYSQL_USER"],
		"-D", config.Env["MYSQL_DATABASE"],
	}, args.All["<argument>"].([]string)...)
	return runJob(client, *config)
}

func runMysqlDump(args *docopt.Args, client *controller.Client, config *runConfig) error {
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

	configMysqlDump(config)
	return runJob(client, *config)
}

func configMysqlDump(config *runConfig) {
	config.Entrypoint = []string{"mysqldump"}
	config.Args = []string{
		"-h", config.Env["MYSQL_HOST"],
		"-u", config.Env["MYSQL_USER"],
		"--databases", config.Env["MYSQL_DATABASE"],
		"--no-create-db",
	}
}

func runMysqlRestore(args *docopt.Args, client *controller.Client, config *runConfig) error {
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
	return mysqlRestore(client, config)
}

func mysqlRestore(client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"mysql"}
	config.Args = []string{"-u", config.Env["MYSQL_USER"], "-D", config.Env["MYSQL_DATABASE"]}
	err := runJob(client, *config)
	if exit, ok := err.(RunExitError); ok && exit == 1 {
		return nil
	}
	return err
}
