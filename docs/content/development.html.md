---
title: Development
layout: docs
---

# Development

This guide will explain how to:

* Make changes to the Flynn source code
* Build and run Flynn
* Run the tests
* Create a release of Flynn

## Development environment

Development work is typically done inside a [VirtualBox](https://www.virtualbox.org/) VM managed
by [Vagrant](https://www.vagrantup.com/), and Flynn includes a Vagrantfile which fully automates
the creation of the VM.

### Running the development VM

If you don't already have VirtualBox and Vagrant installed, you should
install them by following the directions on their respective web sites.

Clone the Flynn source code locally:

```
$ git clone https://github.com/flynn/flynn.git
```

Then, inside the `flynn` directory, bring up the VM:

```
$ vagrant up
```

If this is the first time you are creating the VM, Vagrant will need to download the
underlying VirtualBox files which are ~1GB in size, so this could take a while depending on
your internet connection.

Once that command has finished, you will have a VM running which you can SSH to:

```
$ vagrant ssh
```

From now on, it is assumed that commands will be run inside the VM, unless otherwise stated.

## Making code changes

The development VM is configured to share the Flynn source code from your machine and mount
it at `/vagrant`, meaning you can edit files locally on your machine and those changes
will be visible inside the VM.

Since Flynn is primarily written in Go, the source code needs to be inside a valid Go workspace.
The development VM has a `GOPATH` of `$HOME/go` and the Flynn source code is symlinked from
`/vagrant` to `$GOPATH/src/github.com/flynn/flynn`.

If you don't have a specific issue you are trying to fix, but are interested in contributing
to the project, you should start by looking at GitHub issues labelled
[easy](https://github.com/flynn/flynn/labels/easy).

## Building Flynn

We use the [tup](http://gittup.org/tup/) build system to run the commands which build the various
components of Flynn.

To kickoff the build process, just run `make`:

```
$ make
```

This will do things like build Go binaries and create Docker images. If you're interested in
exactly what will be built, take a look at the `Tupfiles` in various subdirectories.

If any build command fails, `tup` will output an error and abort the entire build. You can then
fix the issue and then re-run `make`.

If you want to rebuild all Go binaries, run `make clean`.

Once tup runs successfully, you will have a number of built Go binaries and Docker images which
can be used to run Flynn.

## Running Flynn

Once you have built all the Flynn components, you can boot a single node Flynn cluster by running
the following script:

```
$ script/bootstrap-flynn
```

This will do the following:

* stop the `flynn-host` daemon and any running Flynn services
* start the `flynn-host` daemon, which will in turn start etcd and discoverd
* run the Flynn bootstrapper, which will start all the Flynn Layer 1 services

If you want to boot Flynn using a different job backend, or an external IP other
than that of the `eth0` device, the script provides some options for doing so.
See `script/bootstrap-flynn -h` for a full list of supported options.

Once Flynn is running, you can add the cluster to the `flynn` CLI tool using the
bootstrap output, and then try out your changes (e.g. by following [this guide]
(https://github.com/flynn/flynn#trying-it-out)).

## Debugging

If things don't seem to be running as expected, here are some useful commands to help
diagnose the issue:

### check the flynn-host daemon log

```
$ less /tmp/flynn-host.log
```

### view running jobs

```
$ flynn-host ps
ID                                                      STATE    STARTED             CONTROLLER APP  CONTROLLER TYPE
flynn-66f3ca0c60374a1abb172e3a73b50e21                  running  About a minute ago  example         web
flynn-9d716860f69f4f63bfb4074bcc7f4419                  running  4 minutes ago       gitreceive      app
flynn-7eff6d37af3c4d909565ca0ab3b077ad                  running  4 minutes ago       router          app
flynn-b8f3ecd48bb343dab96744a17c96b95d                  running  4 minutes ago       blobstore       web
...
```

### view all jobs (i.e. running + stopped)

```
$ flynn-host ps -a
ID                                                      STATE    STARTED             CONTROLLER APP  CONTROLLER TYPE
flynn-7fd8c48542e442349c0217e7cb52dec9                  running  15 seconds ago      example         web
flynn-66f3ca0c60374a1abb172e3a73b50e21                  running  About a minute ago  example         web
flynn-9868a539703145a1886bc2557f6f6441                  done     2 minutes ago       example         web
flynn-100f36e9d18849658e11188a8b85e79f                  done     3 minutes ago       example         web
flynn-f737b5ece2694f81b3d5efdc2cb8dc56                  done     4 minutes ago
flynn-9d716860f69f4f63bfb4074bcc7f4419                  running  5 minutes ago       gitreceive      app
flynn-7eff6d37af3c4d909565ca0ab3b077ad                  running  5 minutes ago       router          app
flynn-b8f3ecd48bb343dab96744a17c96b95d                  running  5 minutes ago       blobstore       web
...
```

### view the output of a job

```
$ flynn-host log $JOBID
Listening on 55006
```

### inspect a job

```
$ flynn-host inspect $JOBID
ID                           flynn-075c21e8b79a41d89713352f04f94a71
Status                       running
StartedAt                    2014-10-14 14:34:11.726864147 +0000 UTC
EndedAt                      0001-01-01 00:00:00 +0000 UTC
ExitStatus                   0
IP Address                   192.168.200.24
flynn-controller.release     954b7ee40ef24a1798807499d5eb8297
flynn-controller.type        web
flynn-controller.app         e568286366d443c49dc18e7a99f40fc1
flynn-controller.app_name    example
```

### stop a job

```
$ flynn-host stop $JOBID
```

### stop all jobs

```
$ flynn-host ps -a | xargs flynn-host stop
```

*(NOTE: as jobs are stopped, the scheduler may start new jobs. To avoid this, stop the scehduler first)*

### stop all jobs for a particular app

Assuming the app has name `example`:

```
$ flynn-host ps | awk -F " {2,}" '$4=="example" {print $1}' | xargs flynn-host stop
```

### upload logs and system information to a gist

If you want to get help diagnosing issues on your system, run the following to upload some
useful information to an anonymous gist:

```
$ flynn-host upload-debug-info
14:45:55.596680 upload-debug-info.go:69: Debug information uploaded to: https://gist.github.com/877117544439b0acaf0e
```

You can then post the gist in the `#flynn` IRC room when asking for assistance to make it easier for
someone to help you.

## Running tests

Flynn has two types of tests:

* "unit" tests which are run using `go test`
* "integration" tests which run against a booted Flynn cluster

### Run the unit tests

To run all the unit tests:


```
$ go test ./...
```

To run tests for an individual component (e.g. the router):

```
$ go test ./router
```

To run tests for a component and all sub-components (e.g. the controller):

```
$ go test ./controller/...
```

### Run the integration tests

The integration tests live in the `tests` directory, and require a running Flynn
cluster before they can run.

To run all the integration tests:

```
$ script/run-integration-tests
```

This will:

* Run `make` to build Flynn
* Boot a single node Flynn cluster by running `script/bootstrap-flynn`
* Run the integration test binary (i.e. `bin/flynn-test`)

To run an individual integration test (e.g. `TestEnvDir`):

```
$ script/run-integration-tests -f TestEnvDir
```

## Pull request

Once you have made changes to the Flynn source code and tested your changes, you
should open a pull request on GitHub so we can review your changes and merge
them into the Flynn repository.

Please see the [contribution guide](/docs/contributing) for more information.

## Releasing Flynn

Once you have built and tested Flynn inside the development VM, you can create a release
and install the components on other hosts (e.g. in EC2).

A Flynn release is a set of components consisting of binaries, configuration files and
filesystem images, all of which must be installed in order to run Flynn.

### The Update Framework (TUF)

Flynn uses [The Update Framework](http://theupdateframework.com/) (also known as TUF) to
securely distribute all of the Flynn components.

To release Flynn, you will need to generate a TUF repository using the
[go-tuf](https://github.com/flynn/go-tuf) library.

Follow the [installation instructions](https://github.com/flynn/go-tuf#install)
and then follow the ["Create signed root manifest"](https://github.com/flynn/go-tuf#examples)
example. You should now have a directory with the following layout:


```
.
├── keys
│   ├── snapshot.json
│   ├── targets.json
│   └── timestamp.json
├── repository
└── staged
    ├── root.json
    └── targets
```

The TUF root keys need to be compiled into the Flynn release, and image URLs must
be relative to the file server the TUF repository will be uploaded to. This can be
accomplished by setting `CONFIG_TUF_ROOT_KEYS` and `CONFIG_IMAGE_REPOSITORY` in
the `tup.config` file then re-running `make`, e.g.:

```
CONFIG_IMAGE_REPOSITORY=https://s3.amazonaws.com/my-flynn-repo/tuf
CONFIG_TUF_ROOT_KEYS=[{"keytype":"ed25519","keyval":{"public":"31351ecc833417968faabf98e004d1ef48ecfd996f971aeed399a7dc735d2c8c"}}]
```

*The TUF root keys can be determined by running `tuf root-keys` in the TUF repository.*

### Export components

Export the Flynn components into the TUF repository (this will prompt for TUF key
passphrases):

```
$ script/export-components /path/to/tuf-repo
```

### Upload components

Upload the `repository` directory of the TUF repo to the file server referenced
by `CONFIG_IMAGE_REPOSITORY`.

For example, if using S3 and `CONFIG_IMAGE_REPOSITORY` is set to
`https://s3.amazonaws.com/my-flynn-repo/tuf`, use `s3cmd` to sync the files:

```
$ s3cmd sync --acl-public /path/to/tuf-repo/repository s3://my-flynn-repo/tuf
```

You can now distribute `script/install-flynn` and run it with an explicit repo URL
to install the custom built Flynn components:

```
install-flynn -r https://s3.amazonaws.com/my-flynn-repo
```
