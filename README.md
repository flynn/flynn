# flynn-cli

flynn-cli is the command-line client for
[flynn-controller](https://github.com/flynn/flynn-controller). It provides
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

## Credits

flynn-cli is a fork of Heroku's [hk](https://github.com/heroku/hk).

## Flynn

[Flynn](https://flynn.io) is a modular, open source Platform as a Service (PaaS).

If you're new to Flynn, start [here](https://github.com/flynn/flynn).

### Status

Flynn is in active development and **currently unsuitable for production** use.

Users are encouraged to experiment with Flynn but should assume there are stability, security, and performance weaknesses throughout the project. This warning will be removed when Flynn is ready for production use.

Please report bugs as issues on the appropriate repository. If you have a general question or don't know which repo to use, report them [here](https://github.com/flynn/flynn/issues).

## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Apollic Software, LLC.
