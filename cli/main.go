package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-docopt"
	cfg "github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/controller/client"
)

var (
	flagCluster = os.Getenv("FLYNN_CLUSTER")
	flagApp     string
)

func main() {
	log.SetFlags(0)

	usage := `usage: flynn [-a <app>] <command> [<args>...]

Options:
   -a <app>
   -h, --help

Commands:
   help                show usage for a specific command
   cluster             manage clusters
   create              create an app
   apps                list apps
   ps                  list jobs
   kill                kill a job
   log                 get job log
   scale               change formation
   run                 run a job
   env                 manage env variables
   route               manage routes
   provider            manage resource providers
   resource            provision a new resource
   key                 manage SSH public keys
   release             add a docker image release
   version             show flynn version

See 'flynn help <command>' for more information on a specific command.
	`
	args, _ := docopt.Parse(usage, nil, true, Version, true)

	cmd := args.String["<command>"]
	cmdArgs := args.All["<args>"].([]string)

	if cmd == "help" {
		if len(cmdArgs) == 0 { // `flynn help`
			fmt.Println(usage)
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
		runUpdate(cmdArgs)
		return
	} else if updater != nil {
		defer updater.backgroundRun() // doesn't run if os.Exit is called
	}

	flagApp = args.String["-a"]
	if flagApp != "" {
		if err := readConfig(); err != nil {
			log.Fatal(err)
		}

		if ra, err := appFromGitRemote(flagApp); err == nil {
			clusterConf = ra.Cluster
			flagApp = ra.Name
		}
	}

	if err := runCommand(cmd, cmdArgs); err != nil {
		log.Fatal(err)
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
	case func(*docopt.Args, *controller.Client) error, func(*docopt.Args) error, func() error, func():
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
	case func(*docopt.Args, *controller.Client) error:
		// create client and run command
		var client *controller.Client
		cluster, err := getCluster()
		if err != nil {
			log.Fatal(err)
		}
		if cluster.TLSPin != "" {
			pin, err := base64.StdEncoding.DecodeString(cluster.TLSPin)
			if err != nil {
				log.Fatalln("error decoding tls pin:", err)
			}
			client, err = controller.NewClientWithPin(cluster.URL, cluster.Key, pin)
		} else {
			client, err = controller.NewClient(cluster.URL, cluster.Key)
		}
		if err != nil {
			log.Fatal(err)
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
	p := os.Getenv("FLYNNRC")
	if p == "" {
		p = filepath.Join(homedir(), ".flynnrc")
	}
	return p
}

func readConfig() (err error) {
	if config != nil {
		return nil
	}
	config, err = cfg.ReadFile(configPath())
	if os.IsNotExist(err) {
		err = nil
	}
	return
}

func homedir() string {
	home := os.Getenv("HOME")
	if home == "" && runtime.GOOS == "windows" {
		return os.Getenv("%APPDATA%")
	}
	return home
}

var ErrNoClusters = errors.New("no clusters configured")

func getCluster() (*cfg.Cluster, error) {
	if clusterConf != nil {
		return clusterConf, nil
	}
	if err := readConfig(); err != nil {
		return nil, err
	}
	if len(config.Clusters) == 0 {
		return nil, ErrNoClusters
	}
	if flagCluster == "" {
		clusterConf = config.Clusters[0]
		return clusterConf, nil
	}
	for _, s := range config.Clusters {
		if s.Name == flagCluster {
			clusterConf = s
			return s, nil
		}
	}
	return nil, fmt.Errorf("unknown cluster %q", flagCluster)
}

var appName string

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
		log.Fatal(err)
	}
	return name
}

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 1, 2, 2, ' ', 0)
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
