package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
)

func init() {
	register("cluster", runCluster, `
usage: flynn cluster
       flynn cluster add [-f] [-d] [-g <githost>] [-p <tlspin>] <cluster-name> <url> <key>
       flynn cluster remove <cluster-name>
       flynn cluster default [<cluster-name>]

Manage clusters in the ~/.flynnrc configuration file.

Options:
	-f, --force               force add cluster
	-d, --default             set as default cluster
	-g, --git-host <githost>  git host (if host differs from api URL host)
	-p, --tls-pin <tlspin>    SHA256 of the cluster's TLS cert (useful if it is self-signed)

Commands:
	With no arguments, shows a list of clusters.

	add      adds a cluster to the ~/.flynnrc configuration file
	remove   removes a cluster from the ~/.flynnrc configuration file
	default  set or print the default cluster

Examples:

	$ flynn cluster add -g dev.localflynn.com:2222 -p KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs= default https://controller.dev.localflynn.com e09dc5301d72be755a3d666f617c4600
	Cluster "default" added.
`)
}

func runCluster(args *docopt.Args) error {
	if err := readConfig(); err != nil {
		return err
	}

	if args.Bool["add"] {
		return runClusterAdd(args)
	} else if args.Bool["remove"] {
		return runClusterRemove(args)
	} else if args.Bool["default"] {
		return runClusterDefault(args)
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "NAME", "URL")
	for _, s := range config.Clusters {
		if s.Name == config.Default {
			listRec(w, s.Name, s.URL, "(default)")
		} else {
			listRec(w, s.Name, s.URL)
		}
	}
	return nil
}

func runClusterAdd(args *docopt.Args) error {
	s := &cfg.Cluster{
		Name:    args.String["<cluster-name>"],
		URL:     args.String["<url>"],
		Key:     args.String["<key>"],
		GitHost: args.String["--git-host"],
		TLSPin:  args.String["--tls-pin"],
	}

	if err := config.Add(s, args.Bool["--force"]); err != nil {
		return err
	}

	setDefault := args.Bool["--default"] || len(config.Clusters) == 1

	if setDefault && !config.SetDefault(s.Name) {
		return errors.New(fmt.Sprintf("Cluster %q does not exist and cannot be set as default.", s.Name))
	}

	if err := config.SaveTo(configPath()); err != nil {
		return err
	}

	if setDefault {
		log.Printf("Cluster %q added and set as default.", s.Name)
	} else {
		log.Printf("Cluster %q added.", s.Name)
	}
	return nil
}

func runClusterRemove(args *docopt.Args) error {
	name := args.String["<cluster-name>"]

	if config.Remove(name) {
		if err := config.SaveTo(configPath()); err != nil {
			return err
		}

		log.Printf("Cluster %q removed.", name)
	}

	return nil
}

func runClusterDefault(args *docopt.Args) error {
	name := args.String["<cluster-name>"]

	if name == "" {
		log.Printf("%q is default cluster.", config.Default)
		return nil
	}

	if !config.SetDefault(name) {
		log.Printf("Cluster %q not found.", name)
		return nil
	}
	if err := config.SaveTo(configPath()); err != nil {
		return err
	}

	log.Printf("%q is now the default cluster.", name)
	return nil
}
