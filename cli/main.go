package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/docker/go-units"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
	"github.com/flynn/flynn/pkg/shutdown"
	"github.com/flynn/flynn/pkg/version"
	"github.com/flynn/go-docopt"
)

var (
	flagCluster = os.Getenv("FLYNN_CLUSTER")
	flagApp     string
)

func main() {
	defer shutdown.Exit()

	log.SetFlags(0)

	usage := `
usage: flynn [-a <app>] [-c <cluster>] <command> [<args>...]

Options:
	-a <app>
	-c <cluster>
	-h, --help

Commands:
	help        show usage for a specific command
	install     install flynn
	cluster     manage clusters
	create      create an app
	delete      delete an app
	apps        list apps
	info        show app information
	ps          list jobs
	kill        kill jobs
	log         get app log
	scale       change formation
	run         run a job
	env         manage env variables
	limit       manage resource limits
	meta        manage app metadata
	route       manage routes
	pg          manage postgres database
	mysql       manage mysql database
	mongodb     manage mongodb database
	redis       manage redis database
	provider    manage resource providers
	docker      deploy Docker images to a Flynn cluster
	remote      manage git remotes
	resource    provision a new resource
	release     manage app releases
	deployment  list deployments
	volume      manage volumes
	export      export app data
	import      create app from exported data
	version     show flynn version

See 'flynn help <command>' for more information on a specific command.
`[1:]
	args, _ := docopt.Parse(usage, nil, true, version.String(), true)

	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if cmd == "help" {
		if len(cmdArgs) == 0 { // `flynn help`
			fmt.Println(usage)
			return
		} else if cmdArgs[0] == "--json" {
			cmds := make(map[string]string)
			for name, cmd := range commands {
				cmds[name] = cmd.usage
			}
			out, err := json.MarshalIndent(cmds, "", "\t")
			if err != nil {
				shutdown.Fatal(err)
			}
			fmt.Println(string(out))
			return
		} else { // `flynn help <command>`
			cmd = cmdArgs[0]
			cmdArgs = make([]string, 1)
			cmdArgs[0] = "--help"
		}
	}
	// Run the update command as early as possible to avoid the possibility of
	// installations being stranded without updates due to errors in other code
	if cmd == "update" {
		if err := runUpdate(); err != nil {
			shutdown.Fatal(err)
		}
		return
	} else {
		defer updater.backgroundRun() // doesn't run if os.Exit is called
	}

	// Set the cluster config name
	if args.String["-c"] != "" {
		flagCluster = args.String["-c"]
	}

	flagApp = args.String["-a"]
	if flagApp != "" {
		if err := readConfig(); err != nil {
			shutdown.Fatal(err)
		}

		if ra, err := appFromGitRemote(flagApp); err == nil {
			clusterConf = ra.Cluster
			flagApp = ra.Name
		}
	}

	if err := runCommand(cmd, cmdArgs); err != nil {
		log.Println(err)
		shutdown.ExitWithCode(1)
		return
	}
}

type command struct {
	usage     string
	f         interface{}
	optsFirst bool
}

var commands = make(map[string]*command)

func register(cmd string, f interface{}, usage string) *command {
	switch f.(type) {
	case func(*docopt.Args, controller.Client) error, func(*docopt.Args) error, func() error, func():
	default:
		panic(fmt.Sprintf("invalid command function %s '%T'", cmd, f))
	}
	c := &command{usage: strings.TrimLeftFunc(usage, unicode.IsSpace), f: f}
	commands[cmd] = c
	return c
}

func runCommand(name string, args []string) (err error) {
	argv := make([]string, 1, 1+len(args))
	argv[0] = name
	argv = append(argv, args...)

	cmd, ok := commands[name]
	if !ok {
		return fmt.Errorf("%s is not a flynn command. See 'flynn help'", name)
	}
	parsedArgs, err := docopt.Parse(cmd.usage, argv, true, "", cmd.optsFirst)
	if err != nil {
		return err
	}

	switch f := cmd.f.(type) {
	case func(*docopt.Args, controller.Client) error:
		// create client and run command
		client, err := getClusterClient()
		if err != nil {
			shutdown.Fatal(err)
		}

		return f(parsedArgs, client)
	case func(*docopt.Args) error:
		return f(parsedArgs)
	case func() error:
		return f()
	case func():
		f()
		return nil
	}

	return fmt.Errorf("unexpected command type %T", cmd.f)
}

var config *cfg.Config
var clusterConf *cfg.Cluster

func configPath() string {
	return cfg.DefaultPath()
}

func readConfig() (err error) {
	if config != nil {
		return nil
	}
	config, err = cfg.ReadFile(configPath())
	if os.IsNotExist(err) {
		err = nil
	}
	if config.Upgrade() {
		if err := config.SaveTo(configPath()); err != nil {
			return fmt.Errorf("Error saving upgraded config: %s", err)
		}
	}
	return
}

func getClusterClient() (controller.Client, error) {
	cluster, err := getCluster()
	if err != nil {
		return nil, err
	}
	return cluster.Client()
}

var ErrNoClusters = errors.New("no clusters configured")

func getCluster() (*cfg.Cluster, error) {
	app() // try to look up and cache app/cluster from git remotes
	if clusterConf != nil {
		return clusterConf, nil
	}
	if err := readConfig(); err != nil {
		return nil, err
	}
	if len(config.Clusters) == 0 {
		return nil, ErrNoClusters
	}
	name := flagCluster
	// Get the default cluster
	if name == "" {
		name = config.Default
	}
	// Default cluster not set, pick the first one
	if name == "" {
		clusterConf = config.Clusters[0]
		return clusterConf, nil
	}
	for _, s := range config.Clusters {
		if s.Name == name {
			clusterConf = s
			return s, nil
		}
	}
	return nil, fmt.Errorf("unknown cluster %q", name)
}

func app() (string, error) {
	if flagApp != "" {
		return flagApp, nil
	}
	if app := os.Getenv("FLYNN_APP"); app != "" {
		flagApp = app
		return app, nil
	}
	if err := readConfig(); err != nil {
		return "", err
	}

	ra, err := appFromGitRemote(remoteFromGitConfig())
	if err != nil {
		return "", err
	}
	if ra == nil {
		return "", errors.New("no app found, run from a repo with a flynn remote or specify one with -a")
	}
	clusterConf = ra.Cluster
	flagApp = ra.Name
	return ra.Name, nil
}

func mustApp() string {
	name, err := app()
	if err != nil {
		log.Println(err)
		shutdown.ExitWithCode(1)
	}
	return name
}

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
}

func humanTime(ts *time.Time) string {
	if ts == nil || ts.IsZero() {
		return ""
	}
	return units.HumanDuration(time.Now().UTC().Sub(*ts)) + " ago"
}

func listRec(w io.Writer, a ...interface{}) {
	for i, x := range a {
		fmt.Fprint(w, x)
		if i+1 < len(a) {
			w.Write([]byte{'\t'})
		} else {
			w.Write([]byte{'\n'})
		}
	}
}

func compatCheck(client controller.Client, minVersion string) (bool, error) {
	status, err := client.Status()
	if err != nil {
		return false, err
	}
	v := version.Parse(status.Version)
	return v.Dev || !v.Before(version.Parse(minVersion)), nil
}
