/*
Utilities to make it easier to make well-behaved, production CLI applications

	import (
		"github.com/inconshreveable/axiom"
		"github.com/codegangsta/cli"
	)

	func main() {
		app := cli.NewApp()
		app.Name = "ctl"
		app.Usage = "control service"
		app.Commands = []cli.Command{
			{
				Name: "start",
				Action: func(c *cli.Context) {
					fmt.Println("starting service")
				},
			},
			{
				Name: "stop",
				Action: func(c *cli.Context) {
					fmt.Println("stopping service")
				},
			}
		}

		// Wrap all commands with:
		//  - flags to configure logging
		//  - custom crash handling
		//  - graceful handling of invocation from a GUI shell
		axiom.WrapApp(app, axiom.NewMousetrap(), axiom.NewLogged())

		// Use axiom to add version and update commands
		app.Commands = append(app.Commands,
			axiom.VersionCommand(),
			axiom.NewUpdater(equinoxAppId, updatePublicKey).Command(),
		)

		app.Run(os.Args)
	}
*/
package axiom

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/codegangsta/cli"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/inconshreveable/mousetrap"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/mattn/go-colorable"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/go-update.v0"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/go-update.v0/check"
	log "github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/inconshreveable/log15.v2/term"
	"github.com/flynn/flynn/Godeps/_workspace/src/gopkg.in/yaml.v1"
)

type CmdWrapper interface {
	Wrap(cli.Command) cli.Command
}

func Wrap(cmd cli.Command, wrappers ...CmdWrapper) cli.Command {
	for _, w := range wrappers {
		cmd = w.Wrap(cmd)
	}
	return cmd
}

func WrapAll(cmds []cli.Command, wrappers ...CmdWrapper) []cli.Command {
	out := make([]cli.Command, len(cmds))
	for i, c := range cmds {
		out[i] = Wrap(c, wrappers...)
	}
	return out
}

func WrapApp(app *cli.App, wrappers ...CmdWrapper) {
	app.Commands = WrapAll(app.Commands, wrappers...)
}

type Logged struct {
	// Loggers to configure
	Loggers []log.Logger
	// default logging format
	DefaultFormat string
	// default logging level
	DefaultLevel string
	// default logging target ('stdout', 'stderr', 'false', or filesystem path)
	DefaultTarget string
}

func NewLogged() *Logged {
	return &Logged{
		Loggers:       []log.Logger{log.Root()},
		DefaultFormat: "term",
		DefaultLevel:  "info",
		DefaultTarget: "stdout",
	}
}

func (w *Logged) Wrap(cmd cli.Command) cli.Command {
	cmd.Flags = append(cmd.Flags, []cli.Flag{
		cli.StringFlag{"log", w.DefaultTarget, "path to log file, 'stdout', 'stderr' or 'false'", "", nil},
		cli.StringFlag{"log-level", w.DefaultLevel, "logging level", "", nil},
		cli.StringFlag{"log-format", w.DefaultFormat, "log record format: 'term', 'logfmt', 'json'", "", nil},
	}...)
	oldAction := cmd.Action
	cmd.Action = func(c *cli.Context) {
		handler, err := w.HandlerFor(c.String("log"), c.String("log-level"), c.String("log-format"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		for _, l := range w.Loggers {
			l.SetHandler(handler)
		}
		oldAction(c)
	}
	return cmd
}

func (w *Logged) HandlerFor(target, level, format string) (log.Handler, error) {
	lvl, err := log.LvlFromString(level)
	if err != nil {
		return nil, fmt.Errorf("Invalid log level: %v", err)
	}

	var logformat log.Format
	switch format {
	case "json":
		logformat = log.JsonFormat()
	case "logfmt":
		logformat = log.LogfmtFormat()
	case "terminal", "term":
		switch {
		case target == "stdout" && term.IsTty(os.Stdout.Fd()):
			logformat = log.TerminalFormat()
		case target == "stderr" && term.IsTty(os.Stderr.Fd()):
			logformat = log.TerminalFormat()
		default:
			logformat = log.LogfmtFormat()
		}
	default:
		return nil, fmt.Errorf("Invalid log format: %v", format)
	}

	var handler log.Handler
	switch target {
	case "stdout":
		handler = log.StreamHandler(colorable.NewColorableStdout(), logformat)
	case "stderr":
		handler = log.StreamHandler(colorable.NewColorableStderr(), logformat)
	case "false":
		handler = log.DiscardHandler()
	default:
		handler, err = log.FileHandler(target, logformat)
		if err != nil {
			return nil, fmt.Errorf("Failed to open log file '%s': %v", target, err)
		}
	}

	return log.LvlFilterHandler(lvl, handler), nil
}

func Mousetrap(app *cli.App) {
	oldBefore := app.Before
	app.Before = func(c *cli.Context) error {
		if mousetrap.StartedByExplorer() {
			cmd := exec.Command(os.Args[0], os.Args[1:]...)
			cmd.Env = append(os.Environ(), "MOUSETRAP=1")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
			cmd = exec.Command("cmd.exe", "/K")
			cmd.Env = os.Environ()
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			if err != nil {
				fmt.Println("Failed to execute sub-process. Error:", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
		if oldBefore == nil {
			return nil
		}
		return oldBefore(c)
	}
}

type YAMLConfigLoader struct {
	DefaultPath string
	Config      interface{}
}

func NewYAMLConfigLoader(config interface{}) *YAMLConfigLoader {
	return &YAMLConfigLoader{Config: config}
}

func (w *YAMLConfigLoader) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Failed to open configuration file: %v", err)
	}
	defer f.Close()
	return w.Load(f)
}

func (w *YAMLConfigLoader) Load(rd io.Reader) error {
	buf, err := ioutil.ReadAll(rd)
	if err != nil {
		return fmt.Errorf("Failed to read configuration: %v", err)
	}
	err = yaml.Unmarshal(buf, w.Config)
	if err != nil {
		return fmt.Errorf("Failed to parse configuration: %v", err)
	}
	return nil
}

func (w *YAMLConfigLoader) Wrap(cmd cli.Command) cli.Command {
	cmd.Flags = append(cmd.Flags, []cli.Flag{
		cli.StringFlag{"config", w.DefaultPath, "path to YAML config file", "", nil},
	}...)
	oldAction := cmd.Action
	cmd.Action = func(c *cli.Context) {
		configFile := c.String("config")
		if configFile != "" {
			err := w.LoadFromFile(configFile)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}
		oldAction(c)
	}
	return cmd
}

func VersionCommand() cli.Command {
	return cli.Command{
		Name:  "version",
		Usage: "print the version string",
		Action: func(c *cli.Context) {
			fmt.Printf("%s version %s\n", c.App.Name, c.App.Version)
		},
	}
}

type Updater struct {
	Logger         log.Logger
	EquinoxAppId   string
	PublicKeyPEM   string
	DefaultChannel string
}

func NewUpdater(equinoxAppId, publicKeyPEM string) *Updater {
	logger := log.New()
	logger.SetHandler(log.DiscardHandler())
	return &Updater{
		Logger:         logger,
		EquinoxAppId:   equinoxAppId,
		PublicKeyPEM:   publicKeyPEM,
		DefaultChannel: "stable",
	}
}

func (cmd *Updater) Check(channel, version string) (*update.Update, *check.Result, error) {
	if cmd.EquinoxAppId == "" || cmd.PublicKeyPEM == "" {
		return nil, nil, fmt.Errorf("Application must be built with a public key and equinox.io app id to enable updating")
	}

	up, err := update.New().VerifySignatureWithPEM([]byte(cmd.PublicKeyPEM))
	if err != nil {
		return nil, nil, err
	}

	params := check.Params{
		AppVersion: version,
		AppId:      cmd.EquinoxAppId,
		Channel:    channel,
	}
	result, err := params.CheckForUpdate("https://api.equinox.io/1/Updates", up)
	return up, result, err
}

func (cmd *Updater) Update(channel, version string) (*check.Result, error) {
	up, result, err := cmd.Check(channel, version)
	if err != nil {
		return nil, err
	}

	if err := up.CanUpdate(); err != nil {
		return nil, fmt.Errorf("Insufficient permissions to update, try running as root or Administrator")
	}

	err, errRecover := result.Update()
	if err == check.NoUpdateAvailable {
		cmd.Logger.Info("no update available")
		return nil, err
	} else if err != nil {
		if result == nil {
			cmd.Logger.Error("failed to check for update", "err", err)
		} else {
			cmd.Logger.Error("failed to update", "err", err, "errRecover", errRecover)
			if errRecover != nil {
				err = fmt.Errorf("failed to recover from bad update: %v. Original error: %v", errRecover, err)
			}
		}
		return nil, err
	}

	cmd.Logger.Info("update successful", "version", result.Version)
	return result, nil
}

func (cmd *Updater) Command() cli.Command {
	return cli.Command{
		Name:  "update",
		Usage: "update to the latest version",
		Flags: []cli.Flag{
			cli.StringFlag{"channel", cmd.DefaultChannel, "update to the most recent release on this channel", "", nil},
		},
		Action: func(c *cli.Context) {
			res, err := cmd.Update(c.String("channel"), c.App.Version)
			if err != nil {
				fmt.Println("Update command failed:", err)
				os.Exit(1)
			}
			fmt.Printf("Sucessfully updated to version %s\n", res.Version)
		},
	}
}

type crashHandler struct {
	onCrash func(interface{})
}

// XXX: export when implemented
func newCrashHandler(onCrash func(interface{})) CmdWrapper {
	return &crashHandler{onCrash}
}

func (w *crashHandler) Wrap(cmd cli.Command) cli.Command {
	oldAction := cmd.Action
	cmd.Action = func(c *cli.Context) {
		oldAction(c)
	}
	return cmd
}

func SortCommands(commands []cli.Command) {
	sort.Sort(sortedCommands(commands))
}

type sortedCommands []cli.Command

func (a sortedCommands) Len() int           { return len(a) }
func (a sortedCommands) Less(i, j int) bool { return a[i].Name < a[j].Name }
func (a sortedCommands) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func SortFlags(flags []cli.Flag) {
	sort.Sort(sortedFlags(flags))
}

type sortedFlags []cli.Flag

func (a sortedFlags) Len() int           { return len(a) }
func (a sortedFlags) Less(i, j int) bool { return a[i].String() < a[j].String() }
func (a sortedFlags) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
