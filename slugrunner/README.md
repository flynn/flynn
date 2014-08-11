# (Heroku-ish) Slug Runner

A container image that runs Heroku-like
[slugs](https://devcenter.heroku.com/articles/slug-compiler) produced by
[slugbuilder](/slugbuilder).

## What does it do exactly?

It takes a gzipped tarball of a "compiled" application via stdin or from a URL,
letting you run a command in that application environment, or start a process
defined in the application Procfile.

## Using Slug Runner

First, you need Docker. Then you can either pull the image from the public
index:

	$ docker pull flynn/slugrunner

Or you can build from this source:

	$ docker build -t flynn/slugrunner .

When you run the container, it always expects an app slug to be passed via stdin
or by giving it a URL using the SLUG_URL environment variable. Lets run a Rake
task that our app uses, attaching to stdout:

	$ cat myslug.tgz | docker run -i -a stdin -a stdout flynn/slugrunner rake mytask

We can also load slugs using the SLUG_URL environment variable. This is
currently the only way to run interactively, for example running Bash:

	$ docker run -e SLUG_URL=http://example.com/slug.tgz -i -t flynn/slugrunner /bin/bash

Commands are run in the application environment, in the root directory of the
application, with any default environment variables, and scripts sourced from
.profile.d of the application.

Lastly there is a `start` command that will run any of the process types defined
in the Procfile of the app, or of the default process types defined by the
buildpack that built the app. For example, here we can start the `web` process:

	$ cat myslug.tgz | docker run -i -a stdin -a stdout -a stderr flynn/slugrunner start web

## Service Discovery

The runner can also register with [go-discover](/discoverd/client) based service
discovery using [sdutil](/sdutil). If `$SD_NAME` and `$PORT` environment
variables are set, the command is run with `sdutil exec $SD_NAME:$PORT`.
`$SD_NAME` is unset before the command is run, but `$PORT` is left set since it
is often used without service discovery.

It is also possible to fully customize the command line for `sdutil` tool using
`$SD_ARGS`.

## Base Environment

The container image is based on
[cedarish](/util/cedarish), an image that emulates the
Heroku Cedar stack environment. App slugs should include everything they need to
run, but if something is missing it should be added to cedarish.
