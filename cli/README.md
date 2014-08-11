# cli

cli is the command-line client for
[controller](/controller). It provides
access to many functions related to deploying and managing applications.

## Installation

Pre-built binaries are available for Mac OS X, Linux, and BSD. Once installed,
these binaries will automatically update themselves when new releases are
available.

To install a pre-built binary release, run the following one-liner:

```bash
L=/usr/local/bin/flynn && curl -sL -A "`uname -sp`" https://flynn-cli.herokuapp.com/flynn.gz | zcat >$L && chmod +x $L
```

## Usage

The basic usage of flynn-cli is:

```text
flynn [-a app] <command> [options] [arguments]
```

For a list of commands and usage instructions, run `flynn help`.

### Add a server

To add a server to the `~/.flynnrc` configuration file:

```bash
flynn server-add [-g <githost>] [-p <tlspin>] <server-name> <url> <key>
```

### Add a key

To add a ssh public key to the Flynn controller:

```bash
flynn key-add [<public-key-file>]
```

It tries these sources for keys, in order:

1. public-key-file argument, if present
2. output of ssh-add -L, if any
3. file `$HOME/.ssh/id_rsa.pub`

### Create an app

To create an application in Flynn:

```bash
flynn create [<name>]
```

### Scale

To change the number of jobs for each process type in a release:

```bash
flynn scale [-r <release>] <type>=<qty>...
```

For example, if you want to run 3 web processes and 5 workers:

```bash
flynn scale web=2 worker=5
```

### Add a HTTP route

To add a HTTP routes to an application:

```bash
flynn route-add-http [-s <service>] [-c <tls-cert>] [-k <tls-key>] <domain>
```

### List routes

To list routes for your application:

```bash
flynn routes
```

### Remove a route

To remove a route from the Flynn controller:

```bash
flynn route-remove <id>
```

### List processes

To list the running processes:

```bash
flynn ps
```

### View log

To see the log for a specific job:

```bash
flynn log [-s] <job>
```

### Run an one-off process

To run an interactive one-off process:

```bash
flynn run [-d] [-r <release>] <command> [<argument>...]
```

## Development

flynn-cli requires Go 1.2 or newer and uses
[Godep](https://github.com/tools/godep) to manage dependencies. Run `godep go
build` to get a `flynn-cli` binary.


## Credits

flynn-cli is a fork of Heroku's [hk](https://github.com/heroku/hk).
