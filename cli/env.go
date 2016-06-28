package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/go-docopt"
)

func init() {
	register("env", runEnv, `
usage: flynn env [-t <proc>]
       flynn env set [-t <proc>] <var>=<val>...
       flynn env unset [-t <proc>] <var>...
       flynn env get [-t <proc>] <var>

Manage app environment variables.

Options:
	-t, --process-type=<proc>  set or read env for specified process type

Commands:
	With no arguments, shows a list of environment variables.

	set    sets value of one or more env variables
	unset  deletes one or more variables
	get    returns the value of variable

Examples:

	$ flynn env set FOO=bar BAZ=foobar
	Created release 5058ae7964f74c399a240bdd6e7d1bcb.

	$ flynn env
	BAZ=foobar
	FOO=bar

	$ flynn env get -t web FOO
	bar

	$ flynn env unset FOO
	Created release b1bbd9bc76d6436ea2fd245300bce72e.
`)
}

var envProc string

func runEnv(args *docopt.Args, client controller.Client) error {
	envProc = args.String["--process-type"]

	if args.Bool["set"] {
		return runEnvSet(args, client)
	} else if args.Bool["unset"] {
		return runEnvUnset(args, client)
	} else if args.Bool["get"] {
		return runEnvGet(args, client)
	}

	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	if envProc != "" {
		if release.Env == nil {
			release.Env = make(map[string]string)
		}
		for k, v := range release.Processes[envProc].Env {
			release.Env[k] = v
		}
	}

	vars := make([]string, 0, len(release.Env))
	for k, v := range release.Env {
		vars = append(vars, k+"="+v)
	}
	sort.Strings(vars)

	for _, v := range vars {
		fmt.Println(v)
	}
	return nil
}

func runEnvSet(args *docopt.Args, client controller.Client) error {
	pairs := args.All["<var>=<val>"].([]string)
	env := make(map[string]*string, len(pairs))
	for _, s := range pairs {
		v := strings.SplitN(s, "=", 2)
		if len(v) != 2 {
			return fmt.Errorf("invalid var format: %q", s)
		}
		env[v[0]] = &v[1]
	}
	id, err := setEnv(client, envProc, env)
	if err != nil {
		return err
	}
	log.Printf("Created release %s.", id)
	return nil
}

func runEnvUnset(args *docopt.Args, client controller.Client) error {
	vars := args.All["<var>"].([]string)
	env := make(map[string]*string, len(vars))
	for _, s := range vars {
		env[s] = nil
	}
	id, err := setEnv(client, envProc, env)
	if err != nil {
		return err
	}
	log.Printf("Created release %s.", id)
	return nil
}

func runEnvGet(args *docopt.Args, client controller.Client) error {
	arg := args.All["<var>"].([]string)[0]
	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		return errors.New("no app release found")
	}
	if err != nil {
		return err
	}

	if _, ok := release.Processes[envProc]; envProc != "" && !ok {
		return fmt.Errorf("process type %q not found in release %s", envProc, release.ID)
	}

	if v, ok := release.Env[arg]; ok {
		fmt.Println(v)
		return nil
	}
	if v, ok := release.Processes[envProc].Env[arg]; ok {
		fmt.Println(v)
		return nil
	}

	return fmt.Errorf("var %q not found in release %q", arg, release.ID)
}

func setEnv(client controller.Client, proc string, env map[string]*string) (string, error) {
	app, err := client.GetApp(mustApp())
	if err != nil {
		return "", err
	}
	release, err := client.GetAppRelease(app.ID)
	if err == controller.ErrNotFound {
		release = &ct.Release{}
		if proc != "" {
			release.Processes = make(map[string]ct.ProcessType)
			release.Processes[proc] = ct.ProcessType{}
		}
	} else if err != nil {
		return "", err
	}

	var dest map[string]string
	if proc != "" {
		if _, ok := release.Processes[proc]; !ok {
			return "", fmt.Errorf("process %q in release %s not found", proc, release.ID)
		}
		if release.Processes[proc].Env == nil {
			p := release.Processes[proc]
			p.Env = make(map[string]string, len(env))
			release.Processes[proc] = p
		}
		dest = release.Processes[proc].Env
	} else {
		if release.Env == nil {
			release.Env = make(map[string]string, len(env))
		}
		dest = release.Env
	}
	for k, v := range env {
		if v == nil {
			delete(dest, k)
		} else {
			dest[k] = *v
		}
	}

	release.ID = ""
	if err := client.CreateRelease(app.ID, release); err != nil {
		return "", err
	}
	if err := client.DeployAppRelease(app.ID, release.ID, nil); err != nil {
		return "", err
	}
	return release.ID, nil
}
