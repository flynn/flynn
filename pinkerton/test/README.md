Pinkerton Test Files
--------------------

This directory contains test files for running a Docker registry when testing
pinkerton.

Generate the files
==================

Start the registry:

```shell
go run registry.go "$(pwd)/files"
```

Build and push the image:

```shell
docker build -t 127.0.0.1:8080/pinkerton-test ./image
docker push 127.0.0.1:8080/pinkerton-test
```

The registry files will now be in the `files` directory.
