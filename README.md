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


## Components

### Layer 0

#### [flynn-host](https://github.com/flynn/flynn-host)

#### [discoverd](https://github.com/flynn/discoverd)

### Layer 1

#### [flynn-controller](https://github.com/flynn/flynn-controller)

#### [flynn-bootstrap](https://github.com/flynn/flynn-bootstrap)

#### [gitreceived](https://github.com/flynn/gitreceived)

#### [flynn-cli](https://github.com/flynn/flynn-cli)

#### [flynn-receive](https://github.com/flynn/flynn-receive)

#### [slugbuilder](https://github.com/flynn/slugbuilder)

#### [slugrunner](https://github.com/flynn/slugrunner)

#### [flynn-dev](https://github.com/flynn/flynn-dev)

#### [strowger](https://github.com/flynn/strowger)

#### [shelf](https://github.com/flynn/shelf)

#### [stdutil](https://github.com/flynn/stdutil)

#### [flynn-postgres](https://github.com/flynn/flynn-postgres)

#### [grid-cli](https://github.com/flynn/grid-cli)

#### [taffy](https://github.com/flynn/taffy)

### Libraries

#### [go-sql](https://github.com/flynn/gosql)

#### [go-crypto-ssh](https://github.com/flynn/go-crypto-ssh)

#### [go-flynn](https://github.com/flynn/go-flynn)

#### [rpcplus](https://github.com/flynn/rpcplus)

## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Apollic Software, LLC.
