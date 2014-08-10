package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

func init() {
	register("env", runEnv, `usage: flynn env [-t <proc>]
       flynn env set [-t <proc>] <var>=<val>...
       flynn env unset [-t <proc>] <var>...
       flynn env get [-t <proc>] <var>

Manage app environment.

Options:
   -t, --process-type <proc>   include env from process type

Commands:
   With no arguments, shows a list of environment variables.

   set    Sets value of one or more env variables.
   unset  Deletes one or more variables.
   get    Returns the value of variable.
`)
}

var envProc string

func runEnv(args *docopt.Args, client *controller.Client) error {
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

func runEnvSet(args *docopt.Args, client *controller.Client) error {
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

func runEnvUnset(args *docopt.Args, client *controller.Client) error {
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

func runEnvGet(args *docopt.Args, client *controller.Client) error {
	arg := args.All["<var>"].([]string)[0]
	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		return errors.New("no app release found")
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

func setEnv(client *controller.Client, proc string, env map[string]*string) (string, error) {
	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		artifact := &ct.Artifact{}
		if err := client.CreateArtifact(artifact); err != nil {
			return "", err
		}
		release = &ct.Release{ArtifactID: artifact.ID}
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
	if err := client.CreateRelease(release); err != nil {
		return "", err
	}
	return release.ID, client.SetAppRelease(mustApp(), release.ID)
}
