package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
)

func init() {
	register("cluster", runCluster, `
usage: flynn cluster
       flynn cluster add [-f] [-d] [-g <githost>] [--git-url <giturl>] [-p <tlspin>] <cluster-name> <domain> <key>
       flynn cluster remove <cluster-name>
       flynn cluster default [<cluster-name>]

Manage clusters in the ~/.flynnrc configuration file.

Options:
	-f, --force               force add cluster
	-d, --default             set as default cluster
	-g, --git-host=<githost>  git host (legacy SSH only)
	--git-url=<giturl>        git URL
	-p, --tls-pin=<tlspin>    SHA256 of the cluster's TLS cert (useful if it is self-signed)

Commands:
	With no arguments, shows a list of clusters.

	add      adds a cluster to the ~/.flynnrc configuration file
	remove   removes a cluster from the ~/.flynnrc configuration file
	default  set or print the default cluster

Examples:

	$ flynn cluster add -p KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs= default dev.localflynn.com e09dc5301d72be755a3d666f617c4600
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

	listRec(w, "NAME", "CONTROLLER URL", "GIT URL")
	for _, s := range config.Clusters {
		data := []interface{}{s.Name, s.ControllerURL}
		if s.GitHost != "" {
			data = append(data, s.GitHost, "(legacy git)")
		} else {
			data = append(data, s.GitURL)
		}
		if s.Name == config.Default {
			data = append(data, "(default)")
		}
		listRec(w, data...)
	}
	return nil
}

func runClusterAdd(args *docopt.Args) error {
	s := &cfg.Cluster{
		Name:    args.String["<cluster-name>"],
		Key:     args.String["<key>"],
		GitHost: args.String["--git-host"],
		GitURL:  args.String["--git-url"],
		TLSPin:  args.String["--tls-pin"],
	}
	domain := args.String["<domain>"]
	if strings.HasPrefix(domain, "https://") {
		s.ControllerURL = domain
	} else {
		s.ControllerURL = "https://controller." + domain
	}
	if s.GitURL == "" && s.GitHost == "" {
		s.GitURL = "https://git." + domain
	}

	if err := config.Add(s, args.Bool["--force"]); err != nil {
		return err
	}

	setDefault := args.Bool["--default"] || len(config.Clusters) == 1

	if setDefault && !config.SetDefault(s.Name) {
		return errors.New(fmt.Sprintf("Cluster %q does not exist and cannot be set as default.", s.Name))
	}

	if !s.SSHGit() {
		client, err := s.Client()
		if err != nil {
			return err
		}
		caPath, err := writeCACert(client, s.Name)
		if err != nil {
			return fmt.Errorf("Error writing CA certificate: %s", err)
		}
		if err := writeGlobalGitConfig(s.GitURL, caPath); err != nil {
			return err
		}
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

func writeCACert(c *controller.Client, name string) (string, error) {
	res, err := c.RawReq("GET", "/ca-cert", nil, nil, nil)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := os.MkdirAll(caCertDir(), 0755); err != nil {
		return "", err
	}
	dest, err := os.Create(filepath.Join(caCertDir(), name+".pem"))
	if err != nil {
		return "", err
	}
	defer dest.Close()
	_, err = io.Copy(dest, res.Body)
	return dest.Name(), err
}

func runClusterRemove(args *docopt.Args) error {
	name := args.String["<cluster-name>"]

	if c := config.Remove(name); c != nil {
		msg := fmt.Sprintf("Cluster %q removed.", name)

		// Select next available cluster as default
		if config.Default == name && len(config.Clusters) > 0 {
			config.SetDefault(config.Clusters[0].Name)
			msg = fmt.Sprintf("Cluster %q removed and %q is now the default cluster.", name, config.Default)
		}

		if err := config.SaveTo(configPath()); err != nil {
			return err
		}

		if !c.SSHGit() {
			removeGlobalGitConfig(c.GitURL)
		}

		log.Print(msg)
	}

	return nil
}

func runClusterDefault(args *docopt.Args) error {
	name := args.String["<cluster-name>"]

	if name == "" {
		w := tabWriter()
		defer w.Flush()
		listRec(w, "NAME", "URL")
		for _, s := range config.Clusters {
			if s.Name == config.Default {
				listRec(w, s.Name, s.URL, "(default)")
				break
			}
		}
		return nil
	}

	if !config.SetDefault(name) {
		return fmt.Errorf("Cluster %q does not exist and cannot be set as default.", name)
	}
	if err := config.SaveTo(configPath()); err != nil {
		return err
	}

	log.Printf("%q is now the default cluster.", name)
	return nil
}
