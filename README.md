# (Heroku-ish) Slug Runner
A [Docker](http://docker.io) container that runs Heroku-like [slugs](https://devcenter.heroku.com/articles/slug-compiler) produced by [slugbuilder](https://github.com/flynn/slugbuilder).

## What does it do exactly?

It takes a gzipped tarball of a "compiled" application via stdin or from a URL, letting you run a command in that application environment, or start a process defined in the application Procfile.

## Using Slug Runner

First, you need Docker. Then you can either pull the image from the public index:

	$ docker pull flynn/slugrunner

Or you can build from this source:

	$ cd slugrunner
	$ make

When you run the container, it always expects an app slug to be passed via stdin or by giving it a URL using the SLUG_URL environment variable. Let's run Bash interactively with an app slug:

	$ cat myslug.tgz | docker -i -t flynn/slugrunner /bin/bash

Or we could run a Rake task if our app uses Rake, attaching to stdout and this time using a slug at a URL:

	$ docker -e SLUG_URL=http://example.com/slug.tgz -a stdout flynn/slugrunner rake mytask

Commands are run in the application environment, with any default environment variables and scripts sourced from .profile.d of the application. Everything is run from the application root.

Lastly there is a `start` command that will run any of the process types defined in the Procfile of the app, or of the default process types defined by the buildpack that built the app. For example, here we can start the `web` process:

	$ cat myslug.tgz | docker -i -a stdin -a stdout flynn/slugrunner start web

