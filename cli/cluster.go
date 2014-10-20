package main

import (
	"log"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
)

func init() {
	register("cluster", runCluster, `
usage: flynn cluster
       flynn cluster add [-g <githost>] [-p <tlspin>] <cluster-name> <url> <key>
       flynn cluster remove <cluster-name>

Manage clusters in the ~/.flynnrc configuration file.

Options:
	-g, --git-host <githost>  git host (if host differs from api URL host)
	-p, --tls-pin <tlspin>    SHA256 of the cluster's TLS cert (useful if it is self-signed)

Commands:
	With no arguments, shows a list of clusters.

	add     adds a cluster to the ~/.flynnrc configuration file
	remove  removes a cluster from the ~/.flynnrc configuration file

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
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "NAME", "URL")
	for _, s := range config.Clusters {
		listRec(w, s.Name, s.URL)
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
	if err := config.Add(s); err != nil {
		return err
	}
	if err := config.SaveTo(configPath()); err != nil {
		return err
	}

	log.Printf("Cluster %q added.", s.Name)
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
