---
title: How To Deploy Go
layout: docs
---

# How To Deploy Go

Go apps are supported on Flynn by the [Go
buildpack](https://github.com/kr/heroku-buildpack-go). 

## Detection

The Go buildpack is used if the repository contains any filenames ending with `.go`.

## Dependencies

The Go buildpack allows you to specify and install your dependencies using one
of two methods, `godep` or `go get`. The recommended method is to use
[godep](https://github.com/tools/godep), which saves application dependencies
into the git repo so that the application can be reproducibly deployed.

### godep

To save dependencies using [godep](https://github.com/tools/godep), run `godep save` in your app directory and commit the `Godeps` directory. When you deploy
to Flynn, the packages in the `Godeps` directory will be used.

### go get

If you do not have a `Godeps` directory in your app repo, the buildpack
downloads [Mercurial](http://mercurial.selenic.com/) and
[Bazaar](http://bazaar.canonical.com/en/) and then runs `go get` to download the
latest version of each imported package. This method can be slow and unreliable, as it does not provide repeatable builds.

A `.godir` file must exist in the root of the repository if you are not using
godep. This file must contain the full package path of your application, which
will be used to find packages and name the app binary. For example, a `.godir`
file containing the path `github.com/flynn/flynn` with a main package in the
root will result in a binary called `flynn`.

### Go Version

When you use `godep`, the `GoVersion` property in the `Godeps/Godeps.json` file
is used to specify the Go version.

If you do not use `godep`, the latest version of Go known by the buildpack will
be used.

## Binaries

All main packages in the repo are compiled and binaries placed in the `/app/bin`
directory, which is in the `PATH`. Binaries are named after the directory that
contains them.

If the root of the repo contains a main package, the binary name
is derived from the package path. The path is read from the `ImportPath` property
of `Godeps/Godeps.json` if you are using `godep`, or the `.godir` file if you
are not.

## Process Types

The process types your app supports are declared in a `Procfile` in the root
directory, which contains one line per type in the format `TYPE: COMMAND`.

For example: if you have a main package in the root of your repository, and the
package path is `github.com/flynn/myserver`, the binary will be named
`myserver`, and you should have something like this in your `Procfile`:

```text
web: myserver
```

The `web` process type has an HTTP route by default and a corresponding `PORT`
environment variable that the server should listen on.
