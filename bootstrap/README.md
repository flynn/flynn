# Bootstrap

Bootstrap performs a list of actions against a Flynn cluster. It is
typically used to boot Flynn layer 1 services on a new layer 0 cluster.

## Usage

There is an [template manifest](manifest_template.json) that boots a default
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

By default, the bootstrapper will try to connect to [discoverd](/discoverd) at
`localhost:1111`, use the `DISCOVERD` environment variable to change this.

A machine-readable output format is available by adding the `-json` flag. The
manifest may also be provided via STDIN.
