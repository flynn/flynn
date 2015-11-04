package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/cheggaaa/pb"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/shutdown"
)

func init() {
	register("cluster", runCluster, `
usage: flynn cluster
       flynn cluster add [-f] [-d] [-g <githost>] [--git-url <giturl>] [-p <tlspin>] <cluster-name> <domain> <key>
       flynn cluster remove <cluster-name>
       flynn cluster default [<cluster-name>]
       flynn cluster migrate-domain <domain>
       flynn cluster backup [--file <file>]

Manage Flynn clusters.


Commands:
    With no arguments, shows a list of configured clusters.

    add
        Adds <cluster-name> to the ~/.flynnrc configuration file.

        options:
            -f, --force               force add cluster
            -d, --default             set as default cluster
            -g, --git-host=<githost>  git host (legacy SSH only)
            --git-url=<giturl>        git URL
            -p, --tls-pin=<tlspin>    SHA256 of the cluster's TLS cert

    remove
        Removes <cluster-name> from the ~/.flynnrc configuration file.

    default
        With no arguments, prints the default cluster. With <cluster-name>, sets
        the default cluster.

    migrate-domain
        Migrates the cluster's base domain from the current one to <domain>.

        New certificates will be generated for the controller/dashboard and new
        routes will be added with the pattern <app-name>.<domain> for each app.

    backup
        Takes a backup of the cluster.

        The backup may be restored while creating a new cluster with
        'flynn-host bootstrap --from-backup'.

        options:
            --file=<backup-file>  file to write backup to (defaults to stdout)

Examples:

	$ flynn cluster add -p KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs= default dev.localflynn.com e09dc5301d72be755a3d666f617c4600
	Cluster "default" added.

	$ flynn cluster migrate-domain new.example.com
	Migrate cluster domain from "example.com" to "new.example.com"? (yes/no): yes
	Migrating cluster domain (this can take up to 2m0s)...
	Changed cluster domain from "example.com" to "new.example.com"
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
	} else if args.Bool["migrate-domain"] {
		return runClusterMigrateDomain(args)
	} else if args.Bool["backup"] {
		return runClusterBackup(args)
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
		if err := cfg.WriteGlobalGitConfig(s.GitURL, caPath); err != nil {
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
	dest, err := cfg.CACertFile(name)
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
			cfg.RemoveGlobalGitConfig(c.GitURL)
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

func runClusterMigrateDomain(args *docopt.Args) error {
	client, err := getClusterClient()
	if err != nil {
		shutdown.Fatal(err)
	}

	dm := &ct.DomainMigration{
		Domain: args.String["<domain>"],
	}

	release, err := client.GetAppRelease("controller")
	if err != nil {
		return err
	}
	dm.OldDomain = release.Env["DEFAULT_ROUTE_DOMAIN"]

	if !promptYesNo(fmt.Sprintf("Migrate cluster domain from %q to %q?", dm.OldDomain, dm.Domain)) {
		fmt.Println("Aborted")
		return nil
	}

	maxDuration := 2 * time.Minute
	fmt.Printf("Migrating cluster domain (this can take up to %s)...\n", maxDuration)

	events := make(chan *ct.Event)
	stream, err := client.StreamEvents(controller.StreamEventsOptions{
		ObjectTypes: []ct.EventType{ct.EventTypeDomainMigration},
	}, events)
	if err != nil {
		return nil
	}
	defer stream.Close()

	if err := client.PutDomain(dm); err != nil {
		return err
	}

	timeout := time.After(maxDuration)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return stream.Err()
			}
			var e *ct.DomainMigrationEvent
			if err := json.Unmarshal(event.Data, &e); err != nil {
				return err
			}
			if e.Error != "" {
				fmt.Println(e.Error)
			}
			if e.DomainMigration.FinishedAt != nil {
				fmt.Printf("Changed cluster domain from %q to %q\n", dm.OldDomain, dm.Domain)
				return nil
			}
		case <-timeout:
			return errors.New("timed out waiting for domain migration to complete")
		}
	}
}

func runClusterBackup(args *docopt.Args) error {
	client, err := getClusterClient()
	if err != nil {
		return err
	}

	var bar *pb.ProgressBar
	if term.IsTerminal(os.Stderr.Fd()) {
		bar = pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		bar.ShowBar = false
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
	}

	var dest io.Writer = os.Stdout
	if filename := args.String["--file"]; filename != "" {
		f, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer f.Close()
		dest = f
	}

	fmt.Fprintln(os.Stderr, "Creating cluster backup...")

	tw := NewTarWriter("flynn-backup-"+time.Now().UTC().Format("2006-01-02_150405"), dest)
	defer tw.Close()

	// get app and release details for key apps
	data := make(map[string]*ct.ExpandedFormation, 4)
	for _, name := range []string{"postgres", "discoverd", "flannel", "controller"} {
		app, err := client.GetApp(name)
		if err != nil {
			return fmt.Errorf("error getting %s app details: %s", name, err)
		}
		release, err := client.GetAppRelease(app.ID)
		if err != nil {
			return fmt.Errorf("error getting %s app release: %s", name, err)
		}
		formation, err := client.GetFormation(app.ID, release.ID)
		if err != nil {
			return fmt.Errorf("error getting %s app formation: %s", name, err)
		}
		artifact, err := client.GetArtifact(release.ArtifactID)
		if err != nil {
			return fmt.Errorf("error getting %s app artifact: %s", name, err)
		}
		data[name] = &ct.ExpandedFormation{
			App:       app,
			Release:   release,
			Artifact:  artifact,
			Processes: formation.Processes,
		}
	}
	if err := tw.WriteJSON("flynn.json", data); err != nil {
		return err
	}

	config := &runConfig{
		App:        "postgres",
		Release:    data["postgres"].Release.ID,
		Entrypoint: []string{"sh"},
		Args:       []string{"-c", "pg_dumpall --clean --if-exists | gzip -9"},
		Env: map[string]string{
			"PGHOST":     "leader.postgres.discoverd",
			"PGUSER":     "flynn",
			"PGPASSWORD": data["postgres"].Release.Env["PGPASSWORD"],
		},
		DisableLog: true,
	}
	if err := tw.WriteCommandOutput(client, "postgres.sql.gz", config, bar); err != nil {
		return fmt.Errorf("error dumping database: %s", err)
	}

	if bar != nil {
		bar.Finish()
	}
	fmt.Fprintln(os.Stderr, "Backup complete.")

	return nil
}
