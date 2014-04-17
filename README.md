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

When you run the container, it always expects an app slug to be passed via stdin or by giving it a URL using the SLUG_URL environment variable. Lets run a Rake task that our app uses, attaching to stdout:

	$ cat myslug.tgz | docker run -i -a stdin -a stdout flynn/slugrunner rake mytask

We can also load slugs using the SLUG_URL environment variable. This is currently the only way to run interactively, for example running Bash:

	$ docker run -e SLUG_URL=http://example.com/slug.tgz -i -t flynn/slugrunner /bin/bash

Commands are run in the application environment, in the root directory of the application, with any default environment variables, and scripts sourced from .profile.d of the application.

Lastly there is a `start` command that will run any of the process types defined in the Procfile of the app, or of the default process types defined by the buildpack that built the app. For example, here we can start the `web` process:

	$ cat myslug.tgz | docker run -i -a stdin -a stdout -a stderr flynn/slugrunner start web

## Service Discovery

The runner can also register with [go-discover](https://github.com/flynn/go-discover) based service discovery using [sdutil](https://github.com/flynn/sdutil). If `$SD_NAME` and `$PORT` environment variables are set, the command is run with `sdutil exec $SD_NAME:$PORT`. `$SD_NAME` is unset before the command is run, but `$PORT` is left set since it is often used without service discovery. 

It is also possible to fully customize the command line for `sdutil` tool using `$SD_ARGS`.

## Base Environment

The Docker image here is based on [cedarish](https://github.com/progrium/cedarish), an image that emulates the Heroku Cedar stack environment. App slugs should include everything they need to run, but if something is missing it should be added upstream to cedarish.

## License

BSD

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
