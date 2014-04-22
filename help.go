package main

import (
	"errors"
	"fmt"
	"os"
	"text/template"

	"github.com/flynn/flynn-controller/client"
)

var cmdHelp = &Command{
	NoClient: true,
	Usage:    "help [topic]",
	Long:     `Help shows usage for a command or other topic.`,
}

func init() {
	cmdHelp.Run = runHelp // break init loop
}

func runHelp(cmd *Command, args []string, client *controller.Client) error {
	if len(args) == 0 {
		printUsage()
		return nil // not os.Exit(2); success
	}
	if len(args) != 1 {
		return errors.New("too many arguments")
	}

	for _, cmd := range commands {
		if cmd.Name() == args[0] {
			cmd.printUsage(false)
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown help topic: %q. Run 'flynn help'.\n", args[0])
	os.Exit(2)
	return nil
}

var cmdVersion = &Command{
	Run:      runVersion,
	NoClient: true,
	Usage:    "version",
	Short:    "show flynn version",
	Long:     `Version shows the flynn client version string.`,
}

func runVersion(cmd *Command, args []string, client *controller.Client) error {
	fmt.Println(Version)
	return nil
}

var usageTemplate = template.Must(template.New("usage").Parse(`
Usage: flynn [-a app] [command] [options] [arguments]


Commands:
{{range .Commands}}{{if .Runnable}}{{if .List}}
    {{.Name | printf "%-8s"}}  {{.Short}}{{end}}{{end}}{{end}}

Run 'flynn help [command]' for details.`[1:]))

func printUsage() {
	usageTemplate.Execute(os.Stdout, struct {
		Commands []*Command
	}{
		commands,
	})
}

func usage() {
	printUsage()
	os.Exit(2)
}
