package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/flynn/flynn-controller/client"
	ct "github.com/flynn/flynn-controller/types"
)

var cmdEnv = &Command{
	Run:   runEnv,
	Usage: "env [-t <proc>]",
	Short: "list env vars",
	Long:  "Command env shows all env vars.",
}

var envProc string

func init() {
	cmdEnv.Flag.StringVarP(&envProc, "process-type", "t", "", "include env from process type")
}

func runEnv(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 0 {
		cmd.printUsage(true)
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

var cmdEnvSet = &Command{
	Run:   runEnvSet,
	Usage: "env-set [-t <proc>] <name>=<value>...",
	Short: "set env vars",
	Long:  "Command set sets the value of one or more env vars.",
}

func init() {
	cmdEnvSet.Flag.StringVarP(&envProc, "process-type", "t", "", "set env for process type")
}

func runEnvSet(cmd *Command, args []string, client *controller.Client) error {
	if len(args) == 0 {
		cmd.printUsage(true)
	}

	env := make(map[string]*string, len(args))
	for _, s := range args {
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

var cmdEnvUnset = &Command{
	Run:   runEnvUnset,
	Usage: "env-unset [-t <proc>] <name>...",
	Short: "unset env vars",
	Long:  "Command unset deletes one or more env vars.",
}

func init() {
	cmdEnvUnset.Flag.StringVarP(&envProc, "process-type", "t", "", "unset env var for process type")
}

func runEnvUnset(cmd *Command, args []string, client *controller.Client) error {
	if len(args) == 0 {
		cmd.printUsage(true)
	}

	env := make(map[string]*string, len(args))
	for _, s := range args {
		env[s] = nil
	}
	id, err := setEnv(client, envProc, env)
	if err != nil {
		return err
	}
	log.Printf("Created release %s.", id)
	return nil
}

var cmdEnvGet = &Command{
	Run:   runEnvGet,
	Usage: "env-get [-t <proc>] <name>",
	Short: "get env var",
	Long:  "Get the value of an env var.",
}

func init() {
	cmdEnvGet.Flag.StringVarP(&envProc, "process-type", "t", "", "unset env var for process type")
}

func runEnvGet(cmd *Command, args []string, client *controller.Client) error {
	if len(args) != 1 {
		cmd.printUsage(true)
	}

	release, err := client.GetAppRelease(mustApp())
	if err == controller.ErrNotFound {
		return errors.New("no app release found")
	}

	if _, ok := release.Processes[envProc]; envProc != "" && !ok {
		return fmt.Errorf("process type %q not found in release %s", envProc, release.ID)
	}

	if v, ok := release.Env[args[0]]; ok {
		fmt.Println(v)
		return nil
	}
	if v, ok := release.Processes[envProc].Env[args[0]]; ok {
		fmt.Println(v)
		return nil
	}

	return fmt.Errorf("var %q not found in release %q", args[0], release.ID)
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
