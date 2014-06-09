# pinkerton

pinkerton is a standalone tool for working with Docker images.

Currently it can download images and checkout working copies using the same
local storage format that Docker uses.

## Usage

```text
  pinkerton pull [options] <image-url>
  pinkerton checkout [options] <id> <image-id>
  pinkerton cleanup [options] <id>
  pinkerton -h | --help

Commands:
  pull      Download a Docker image
  checkout  Checkout a working copy of an image
  cleanup   Destroy a working copy of an image

Examples:
  pinkerton pull https://registry.hub.docker.com/redis
  pinkerton pull https://registry.hub.docker.com/ubuntu?tag=trusty
  pinkerton pull https://registry.hub.docker.com/flynn/slugrunner?id=1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton checkout slugrunner-test 1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton cleanup slugrunner-test

Options:
  -h, --help       show this message and exit
  --driver=<name>  storage driver [default: aufs]
  --root=<path>    storage root [default: /var/lib/docker]
```

## Building

Pinkerton requires Go >=1.2 and [Godep](https://github.com/tools/godep) to
build. Clone this repo into `$GOPATH/github.com/flynn/pinkerton` and run `godep
go build` to build a `pinkerton` binary.

## Roadmap

Future projects include more support for introspection, building/editing images,
pushing images to Docker registries, and making the UI more friendly to humans
(it is currently designed for use by robots).


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

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.
