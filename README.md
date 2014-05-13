# Welcome to Flynn

[Flynn](https://flynn.io) is a next generation open source Platform as a Service (PaaS).

Unlike most PaaS's Flynn can run stateful services as well as [12 factor](http://12factor.net/) apps. This includes built-in database appliances (just Postgres to start). Flynn is modular so users can easily modify, upgrade, and replace components.

Flynn components are divided into two _layers_. 

**Layer 0** is a low-level resource framework inspired by the [Google Omega](http://eurosys2013.tudos.org/wp-content/uploads/2013/paper/Schwarzkopf.pdf) paper. Layer 0 also includes service discovery

**Layer 1** is a set of higher level components that makes it easy to deploy and maintain applications and databases.

You can learn more about the project at the [Flynn website](https://flynn.io).

### Status

Flynn is in active development and **currently unsuitable for production** use. 

Users are encouraged to experiment with Flynn but should assume there are stability, security, and performance weaknesses throughout the project. This warning will be removed when Flynn is ready for production use.

Please **report bugs** as issues on the appropriate repository. If you have a general question or don't know which repo to use, report them [here](https://github.com/flynn/flynn/issues).

## Getting Started

We built a [tool](https://flynn.cupcake.io) for launching Flynn clusters on your Amazon Web Services account [here](https://flynn.cupcake.io). 

You can also download a [demo environment](https://github.com/flynn/flynn-demo) for your local machine or learn about the components below.

## Components

### Layer 0

**[flynn-host](https://github.com/flynn/flynn-host)** The Flynn host service

**[discoverd](https://github.com/flynn/discoverd)** The Flynn service discovery system

### Layer 1

**[flynn-controller](https://github.com/flynn/flynn-controller)** The Flynn Controller for the management of applications running on Flynn via an HTTP API

**[flynn-bootstrap](https://github.com/flynn/flynn-bootstrap)** Bootstraps Flynn Layer 1

**[gitreceived](https://github.com/flynn/gitreceived)** An SSH server made specifically for accepting git pushes that will trigger an auth script and then a receiver script to handle the push. (This is a more advanced, standalone version of [gitreceive](https://github.com/progrium/gitreceive).)

**[flynn-cli](https://github.com/flynn/flynn-cli)** Command-line Flynn HTTP API client

**[flynn-receive](https://github.com/flynn/flynn-receive)** Flynn's git deployer

**[slugbuilder](https://github.com/flynn/slugbuilder)** A tool using Docker and [Buildpacks](https://devcenter.heroku.com/articles/buildpacks) to produce a Heroku-like [slug](https://devcenter.heroku.com/articles/slug-compiler) given some application source.

**[slugrunner](https://github.com/flynn/slugrunner)** A Docker container that runs Heroku-like [slugs](https://devcenter.heroku.com/articles/slug-compiler) produced by [slugbuilder](https://github.com/flynn/slugbuilder).

**[flynn-demo](https://github.com/flynn/flynn-demo)** Flynn development environment in a VM

**[strowger](https://github.com/flynn/strowger)** Flynn TCP/HTTP router

**[shelf](https://github.com/flynn/shelf)** A simple, fast HTTP file service

**[sdutil](https://github.com/flynn/sdutil)** Service discovery utility for systems based on go-discover

**[flynn-postgres](https://github.com/flynn/flynn-postgres)** Flynn [PostgreSQL](http://www.postgresql.org/) database appliance

**[taffy](https://github.com/flynn/taffy)** Taffy pulls repos and deploys them to Flynn


### Libraries

**[go-flynn](https://github.com/flynn/go-flynn)**

**[go-discoverd](https://github.com/flynn/go-discoverd)**

**[rpcplus](https://github.com/flynn/rpcplus)**


## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Learn more at [Flynn.io](https://flynn.io).

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Apollic Software, LLC.
