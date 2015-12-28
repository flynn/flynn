package main

import (
	"fmt"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("redis", runRedis, `
usage: flynn redis redis-cli [--] [<argument>...]

Commands:
	redis-cli  Open a console to a Flynn redis instance. Any valid arguments to redis-cli may be provided.

Examples:

    $ flynn redis redis-cli
`)
}

func runRedis(args *docopt.Args, client *controller.Client) error {
	config, err := getAppRedisRunConfig(client)
	if err != nil {
		return err
	}
	switch {
	case args.Bool["redis-cli"]:
		return runRedisCLI(args, client, config)
	}
	return nil
}

func getAppRedisRunConfig(client *controller.Client) (*runConfig, error) {
	appRelease, err := client.GetAppRelease(mustApp())
	if err != nil {
		return nil, fmt.Errorf("error getting app release: %s", err)
	}
	return getRedisRunConfig(client, mustApp(), appRelease)
}

func getRedisRunConfig(client *controller.Client, app string, appRelease *ct.Release) (*runConfig, error) {
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
		Args:       []string{"-h", redisApp + ".discoverd", "-a", appRelease.Env["REDIS_PASSWORD"]},
		DisableLog: true,
		Exit:       true,
	}

	return config, nil
}

func runRedisCLI(args *docopt.Args, client *controller.Client, config *runConfig) error {
	config.Entrypoint = []string{"redis-cli"}
	config.Env["PAGER"] = "less"
	config.Env["LESS"] = "--ignore-case --LONG-PROMPT --SILENT --tabs=4 --quit-if-one-screen --no-init --quit-at-eof"
	config.Args = append(config.Args, args.All["<argument>"].([]string)...)
	return runJob(client, *config)
}
