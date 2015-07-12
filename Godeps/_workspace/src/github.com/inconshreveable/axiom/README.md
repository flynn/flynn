# Axiom - Better CLI applications

An experimental set of tools to make it easier to build production command line applications.

- Command line interfaces and options parsing via [github.com/codegangsta/cli](https://github.com/codegansta/cli)
- Structured, contextual logging via [github.com/inconshreveable/log15](https://github.com/inconshreveable/log15)
- Remote updating via [github.com/inconshreveable/go-update](https://github.com/inconshreveable/go-update) and [equinox.io](https://equinox.io)
- Help for windows users double-clicking your command line application via [github.com/inconshreveable/mousetrap](https://github.com/inconshreveable/mousetrap)
- Easy support for YAML configuration files
- Crash reporting (not implemented yet)

```go
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
```
