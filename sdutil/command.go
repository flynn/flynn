// A simple sub command parser based on the flag package
// https://gist.github.com/srid/3949446
package main

import (
	"flag"
	"fmt"
	"os"
)

type subCommand interface {
	Name() string
	DefineFlags(*flag.FlagSet)
	Run(*flag.FlagSet)
}

type subCommandParser struct {
	cmd subCommand
	fs  *flag.FlagSet
}

func ParseCommands(commands ...subCommand) {
	scp := make(map[string]*subCommandParser, len(commands))
	for _, cmd := range commands {
		name := cmd.Name()
		scp[name] = &subCommandParser{cmd, flag.NewFlagSet(name, flag.ExitOnError)}
		cmd.DefineFlags(scp[name].fs)
	}

	oldUsage := flag.Usage
	flag.Usage = func() {
		oldUsage()
		for name, sc := range scp {
			fmt.Fprintf(os.Stderr, "\n# %s %s\n", os.Args[0], name)
			sc.fs.PrintDefaults()
			fmt.Fprintf(os.Stderr, "\n")
		}
	}

	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	cmdname := flag.Arg(0)
	if sc, ok := scp[cmdname]; ok {
		sc.fs.Parse(flag.Args()[1:])
		sc.cmd.Run(sc.fs)
	} else {
		fmt.Fprintf(os.Stderr, "error: %s is not a valid command", cmdname)
		flag.Usage()
		os.Exit(1)
	}
}
