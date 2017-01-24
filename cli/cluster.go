package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/docker/pkg/term"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/backup"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/go-docopt"
)

func init() {
	register("cluster", runCluster, `
usage: flynn cluster
       flynn cluster add [-f] [-d] [--git-url <giturl>] [--no-git] [--docker-push-url <url>] [--docker] [-p <tlspin>] <cluster-name> <domain> <key>
       flynn cluster remove <cluster-name>
       flynn cluster default [<cluster-name>]
       flynn cluster migrate-domain <domain>
       flynn cluster backup [--file <file>]
       flynn cluster log-sink
       flynn cluster log-sink add syslog [--use-ids] [--insecure] [--format <format>] <url> [<prefix>]
       flynn cluster log-sink remove <id>

Manage Flynn clusters.


Commands:
    With no arguments, shows a list of configured clusters.

    add
        Adds <cluster-name> to the ~/.flynnrc configuration file.

        options:
            -f, --force               force add cluster
            -d, --default             set as default cluster
            --git-url=<giturl>        git URL
            --no-git                  skip git configuration
            --docker-push-url=<url>   Docker push URL
            --docker                  configure Docker to push to the cluster
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

    log-sink
        With no arguments, prints a list of registered log-sinks for this cluster

    log-sink add syslog
        Creates a new syslog log sink with specified <url> and optionally <prefix> template.
        Supported schemes are syslog and syslog+tls

        options:
            --use-ids          Use app IDs instead of app names in the syslog APP-NAME field.
            --insecure         Don't verify servers certificate chain or hostname. Should only be used for testing.
            --format=<format>  One of rfc6587 or newline, defaults to rfc6587.

        examples:
            $ flynn cluster log-sink add syslog syslog+tls://rsyslog.host:514/

    log-sink remove
        Removes a log sink with <id>

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

	if args.Bool["log-sink"] {
		return runLogSink(args)
	} else if args.Bool["add"] {
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

	listRec(w, "NAME", "CONTROLLER URL", "GIT URL", "DOCKER URL")
	for _, s := range config.Clusters {
		gitURL := s.GitURL
		if gitURL == "" {
			gitURL = "(none)"
		}
		dockerURL := s.DockerPushURL
		if dockerURL == "" {
			dockerURL = "(none)"
		}
		data := []interface{}{s.Name, s.ControllerURL, gitURL, dockerURL}
		if s.Name == config.Default {
			data = append(data, "(default)")
		}
		listRec(w, data...)
	}
	return nil
}

func runClusterAdd(args *docopt.Args) error {
	s := &cfg.Cluster{
		Name:          args.String["<cluster-name>"],
		Key:           args.String["<key>"],
		GitURL:        args.String["--git-url"],
		DockerPushURL: args.String["--docker-push-url"],
		TLSPin:        args.String["--tls-pin"],
	}
	domain := args.String["<domain>"]
	if strings.HasPrefix(domain, "https://") {
		s.ControllerURL = domain
	} else {
		s.ControllerURL = "https://controller." + domain
	}
	if s.GitURL == "" && !args.Bool["--no-git"] {
		s.GitURL = "https://git." + domain
	}
	if s.DockerPushURL == "" && args.Bool["--docker"] {
		s.DockerPushURL = "https://docker." + domain
	}

	if err := config.Add(s, args.Bool["--force"]); err != nil {
		return err
	}

	setDefault := args.Bool["--default"] || len(config.Clusters) == 1

	if setDefault && !config.SetDefault(s.Name) {
		return errors.New(fmt.Sprintf("Cluster %q does not exist and cannot be set as default.", s.Name))
	}

	var caPath string
	if s.GitURL != "" || s.DockerPushURL != "" {
		client, err := s.Client()
		if err != nil {
			return err
		}
		caPath, err = writeCACert(client, s.Name)
		if err != nil {
			return fmt.Errorf("Error writing CA certificate: %s", err)
		}
	}

	if s.GitURL != "" {
		if _, err := exec.LookPath("git"); err != nil {
			if serr, ok := err.(*exec.Error); ok && serr.Err == exec.ErrNotFound {
				return errors.New("Executable 'git' was not found. Use --no-git to skip git configuration")
			}
			return err
		}
		if err := cfg.WriteGlobalGitConfig(s.GitURL, caPath); err != nil {
			return err
		}
	}

	if s.DockerPushURL != "" {
		host, err := s.DockerPushHost()
		if err != nil {
			return err
		}
		if err := dockerLogin(host, s.Key); err != nil {
			if e, ok := err.(*exec.Error); ok && e.Err == exec.ErrNotFound {
				err = errors.New("Executable 'docker' was not found.")
			} else if err == ErrDockerTLSError {
				printDockerTLSWarning(host, caPath)
				err = errors.New("Error configuring docker, follow the instructions above then try again")
			}
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

func writeCACert(c controller.Client, name string) (string, error) {
	data, err := c.GetCACert()
	if err != nil {
		return "", err
	}
	dest, err := cfg.CACertFile(name)
	if err != nil {
		return "", err
	}
	defer dest.Close()
	_, err = dest.Write(data)
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

		cfg.RemoveGlobalGitConfig(c.GitURL)

		if host, err := c.DockerPushHost(); err == nil {
			dockerLogout(host)
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
				listRec(w, s.Name, s.ControllerURL, "(default)")
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
	cluster, err := getCluster()
	if err != nil {
		shutdown.Fatal(err)
	}
	client, err := cluster.Client()
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
	stream, err := client.StreamEvents(ct.StreamEventsOptions{
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
				dm = e.DomainMigration
				fmt.Printf("Changed cluster domain from %q to %q\n", dm.OldDomain, dm.Domain)

				// update flynnrc
				cluster.TLSPin = dm.TLSCert.Pin
				cluster.ControllerURL = fmt.Sprintf("https://controller.%s", dm.Domain)
				cluster.GitURL = fmt.Sprintf("https://git.%s", dm.Domain)
				cluster.DockerPushURL = fmt.Sprintf("https://docker.%s", dm.Domain)
				if err := config.SaveTo(configPath()); err != nil {
					return fmt.Errorf("Error saving config: %s", err)
				}

				// update git config
				caFile, err := cfg.CACertFile(cluster.Name)
				if err != nil {
					return err
				}
				defer caFile.Close()
				if _, err := caFile.Write([]byte(dm.TLSCert.CACert)); err != nil {
					return err
				}
				if err := cfg.WriteGlobalGitConfig(cluster.GitURL, caFile.Name()); err != nil {
					return err
				}
				cfg.RemoveGlobalGitConfig(fmt.Sprintf("https://git.%s", dm.OldDomain))

				// try to run "docker login" for the new domain, but just print a warning
				// if it fails so the user can fix it later
				if host, err := cluster.DockerPushHost(); err == nil {
					if err := dockerLogin(host, cluster.Key); err == ErrDockerTLSError {
						printDockerTLSWarning(host, caFile.Name())
					}
				}
				dockerLogout(dm.OldDomain)

				fmt.Println("Updated local CLI configuration")
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
	var progress backup.ProgressBar
	if term.IsTerminal(os.Stderr.Fd()) {
		bar = pb.New(0)
		bar.SetUnits(pb.U_BYTES)
		bar.ShowBar = false
		bar.ShowSpeed = true
		bar.Output = os.Stderr
		bar.Start()
		progress = bar
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

	if err := backup.Run(client, dest, progress); err != nil {
		return err
	}
	if bar != nil {
		bar.Finish()
	}
	fmt.Fprintln(os.Stderr, "Backup complete.")

	return nil
}

func runLogSink(args *docopt.Args) error {
	client, err := getClusterClient()
	if err != nil {
		return err
	}

	if args.Bool["add"] {
		switch {
		case args.Bool["syslog"]:
			return runLogSinkAddSyslog(args, client)
		default:
			return fmt.Errorf("Sink kind not supported")
		}
	}
	if args.Bool["remove"] {
		return runLogSinkRemove(args, client)
	}

	sinks, err := client.ListSinks()
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	listRec(w, "ID", "KIND", "CONFIG")
	for _, j := range sinks {
		listRec(w, j.ID, j.Kind, string(j.Config))
	}

	return nil
}

func runLogSinkAddSyslog(args *docopt.Args, client controller.Client) error {
	u, err := url.Parse(args.String["<url>"])
	if err != nil {
		return fmt.Errorf("Invalid syslog URL: %s", err)
	}
	switch u.Scheme {
	case "syslog", "syslog+tls":
	default:
		return fmt.Errorf("Invalid syslog protocol: %s", u.Scheme)
	}

	var format ct.SyslogFormat
	switch args.String["--format"] {
	case "newline":
		format = ct.SyslogFormatNewline
	case "rfc6587", "":
		format = ct.SyslogFormatRFC6587
	default:
		return fmt.Errorf("Invalid syslog format: %s", args.String["--format"])
	}

	config, _ := json.Marshal(ct.SyslogSinkConfig{
		Prefix:   args.String["<prefix>"],
		URL:      u.String(),
		UseIDs:   args.Bool["--use-ids"],
		Insecure: args.Bool["--insecure"],
		Format:   format,
	})

	sink := &ct.Sink{
		Kind:   ct.SinkKindSyslog,
		Config: config,
	}

	if err := client.CreateSink(sink); err != nil {
		return err
	}

	log.Printf("Created sink %s.", sink.ID)

	return nil
}

func runLogSinkRemove(args *docopt.Args, client controller.Client) error {
	id := args.String["<id>"]

	res, err := client.DeleteSink(id)
	if err != nil {
		return err
	}

	log.Printf("Deleted sink %s.", res.ID)

	return nil
}
