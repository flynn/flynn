# Flynn Command-Line Interface

flynn-cli is the command-line client for the [controller](/controller). It provides
access to many functions related to deploying and managing applications.

## Installation

Pre-built binaries are available for Mac OS X, Linux, and BSD. Once installed,
these binaries will automatically update themselves when new releases are
available.

To install a pre-built binary release, run the following one-liner:

```bash
L=/usr/local/bin/flynn && curl -sL -A "`uname -sp`" https://dl.flynn.io/cli | zcat >$L && chmod +x $L
```

## Usage

The basic usage is:

```text
flynn [-a app] <command> [options] [arguments]
```

For a list of commands and usage instructions, run `flynn help`.

## Credits

flynn-cli is a fork of Heroku's [hk](https://github.com/heroku/hk).
