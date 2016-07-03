package main

import (
	"fmt"
	"io"
	"os"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("mongodb", runMongodb, `
usage: flynn mongodb mongo [--] [<argument>...]
       flynn mongodb dump [-q] [-f <file>]
       flynn mongodb restore [-q] [-f <file>]

Options:
	-f, --file=<file>  name of dump file
	-q, --quiet        don't print progress

Commands:
	mongodb  Open a console to a Flynn mongodb database. Any valid arguments to mongo may be provided.
	dump     Dump a mongo database. If file is not specified, will dump to stdout.
	restore  Restore a database dump. If file is not specified, will restore from stdin.

Examples:

    $ flynn mongodb mongo

    $ flynn mongodb mongo -- --eval "db.users.find()"

    $ flynn mongodb dump -f db.dump

    $ flynn mongodb restore -f db.dump
`)
}

func runMongodb(args *docopt.Args, client controller.Client) error {
	config, err := getAppMongodbRunConfig(client)
	if err != nil {
		return err
	}
	switch {
	case args.Bool["mongo"]:
		return runMongo(args, client, config)
	case args.Bool["dump"]:
		return runMongodbDump(args, client, config)
	case args.Bool["restore"]:
		return runMongodbRestore(args, client, config)
	}
	return nil
}

func getAppMongodbRunConfig(client controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}
	return getMongodbRunConfig(client, mustApp(), appRelease)
}

func getMongodbRunConfig(client controller.Client, app string, appRelease *ct.Release) (*runConfig, error) {
	mongodbApp := appRelease.Env["FLYNN_MONGO"]
	if mongodbApp == "" {
		return nil, fmt.Errorf("No mongodb database found. Provision one with `flynn resource add mongodb`")
	}

	mongodbRelease, err := client.GetAppRelease(mongodbApp)
	if err != nil {
		return nil, fmt.Errorf("error getting mongodb release: %s", err)
	}

	config := &runConfig{
		App:        app,
		Release:    mongodbRelease.ID,
		Env:        make(map[string]string),
		DisableLog: true,
		Exit:       true,
	}
	for _, k := range []string{"MONGO_HOST", "MONGO_USER", "MONGO_PWD", "MONGO_DATABASE"} {
		v := appRelease.Env[k]
		if v == "" {
			return nil, fmt.Errorf("missing %s in app environment", k)
		}
		config.Env[k] = v
	}
	return config, nil
}

func runMongo(args *docopt.Args, client controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"mongo"}
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = append([]string{
		"--host", config.Env["MONGO_HOST"],
		"-u", config.Env["MONGO_USER"],
		"-p", config.Env["MONGO_PWD"],
		"--authenticationDatabase", "admin",
		config.Env["MONGO_DATABASE"],
	}, args.All["<argument>"].([]string)...)

	return runJob(client, *config)
}

func runMongodbDump(args *docopt.Args, client controller.Client, config *runConfig) error {
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
	return mongodbDump(client, config)
}

func configMongodbDump(config *runConfig) {
	config.Entrypoint = []string{"/bin/dump-flynn-mongodb"}
	config.Args = []string{
		"--host", config.Env["MONGO_HOST"],
		"-u", config.Env["MONGO_USER"],
		"-p", config.Env["MONGO_PWD"],
		"--authenticationDatabase", "admin",
		"--db", config.Env["MONGO_DATABASE"],
	}
}

func mongodbDump(client controller.Client, config *runConfig) error {
	configMongodbDump(config)
	return runJob(client, *config)
}

func runMongodbRestore(args *docopt.Args, client controller.Client, config *runConfig) error {
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
	return mongodbRestore(client, config)
}

func mongodbRestore(client controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"/bin/restore-flynn-mongodb"}
	config.Args = []string{
		"--host", config.Env["MONGO_HOST"],
		"-u", config.Env["MONGO_USER"],
		"-p", config.Env["MONGO_PWD"],
		"--authenticationDatabase", "admin",
		"--db", config.Env["MONGO_DATABASE"],
	}
	err := runJob(client, *config)
	if exit, ok := err.(RunExitError); ok && exit == 1 {
		return nil
	}
	return err
}
