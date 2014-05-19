# flynn-bootstrap

flynn-bootstrap performs a list of actions against a Flynn cluster. It is
typically used to boot Flynn layer 1 services on a new layer 0 cluster.

## Usage

There is an [example manifest](bootstrapper/manifest.json) that boots a default
configuration of Flynn layer 1. To run the manifest, you need the `bootstrapper`
binary or `flynn/bootstrap` Docker image and a running Flynn cluster.

To use the Docker image, run:

```text
docker run -e DISCOVERD=<host>:1111 flynn/bootstrap
```

To provide a custom manifest, you can read it from stdin:

```text
cat manifest.json | docker run -i -e DISCOVERD=<host>:1111 flynn/bootstrap -
```

To build the binary, use [godep](https://github.com/tools/godep):

```text
cd bootstrapper
godep go build
./bootstrapper manifest.json
```

Note that the repo must be cloned into the path
`$GOPATH/src/github.com/flynn/flynn-bootstrap` to build.

By default, the bootstrapper will try to connect to
[discoverd](https://github.com/flynn/discoverd) at `localhost:1111`, use the
`DISCOVERD` environment variable to change this.

A machine-readable output format is available by adding the `-json` flag. The
manifest may also be provided via STDIN.

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
