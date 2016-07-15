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
	register("redis", runRedis, `
usage: flynn redis redis-cli [--] [<argument>...]
       flynn redis dump [-q] [-f <file>]
       flynn redis restore [-q] [-f <file>]

Options:
	-f, --file=<file>  name of dump file
	-q, --quiet        don't print progress

Commands:
	redis-cli  Open a console to a Flynn redis instance. Any valid arguments to redis-cli may be provided.
	dump     Dump a redis instance. If file is not specified, will dump to stdout.
	restore  Restore a dump. If file is not specified, will restore from stdin.

Examples:

    $ flynn redis redis-cli

    $ flynn redis dump -f db.dump

    $ flynn redis restore -f db.dump
`)
}

func runRedis(args *docopt.Args, client controller.Client) error {
	config, err := getAppRedisRunConfig(client)
	if err != nil {
		return err
	}
	switch {
	case args.Bool["redis-cli"]:
		return runRedisCLI(args, client, config)
	case args.Bool["dump"]:
		return runRedisDump(args, client, config)
	case args.Bool["restore"]:
		return runRedisRestore(args, client, config)
	}
	return nil
}

func getAppRedisRunConfig(client controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}
	return getRedisRunConfig(client, mustApp(), appRelease)
}

func getRedisRunConfig(client controller.Client, app string, appRelease *ct.Release) (*runConfig, error) {
	redisApp := appRelease.Env["FLYNN_REDIS"]
	if redisApp == "" {
		return nil, fmt.Errorf("No redis server found. Provision one with `flynn resource add redis`")
	}

	redisRelease, err := client.GetAppRelease(redisApp)
	if err != nil {
		return nil, fmt.Errorf("error getting redis release: %s", err)
	}

	config := &runConfig{
		App:        app,
		Release:    redisRelease.ID,
		Env:        make(map[string]string),
		Args:       []string{"redis-cli", "-h", redisApp + ".discoverd", "-a", appRelease.Env["REDIS_PASSWORD"]},
		DisableLog: true,
		Exit:       true,
	}

	return config, nil
}

func runRedisCLI(args *docopt.Args, client controller.Client, config *runConfig) error {
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = append(config.Args, args.All["<argument>"].([]string)...)
	return runJob(client, *config)
}

func runRedisDump(args *docopt.Args, client controller.Client, config *runConfig) error {
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

	config.Args[0] = "/bin/dump-flynn-redis"
	return runJob(client, *config)
}

func runRedisRestore(args *docopt.Args, client controller.Client, config *runConfig) error {
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

	config.Args[0] = "/bin/restore-flynn-redis"
	return runJob(client, *config)
}
