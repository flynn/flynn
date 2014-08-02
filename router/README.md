# Strowger [![Build Status](https://travis-ci.org/flynn/strowger.svg?branch=master)](https://travis-ci.org/flynn/strowger)

[![](https://f.cloud.github.com/assets/13026/2060788/42916822-8c30-11e3-8c0d-ae743b905759.jpg)](https://commons.wikimedia.org/wiki/File:HebdrehwaehlerbatterieOrtsvermittlung_4954.jpg)

Strowger is the Flynn HTTP/TCP cluster router. It relies on [service
discovery](https://github.com/flynn/discoverd) to keep track of what backends
are up and acts as a standard reverse proxy with random load balancing. HTTP
domains and TCP ports are provisioned via RPC. Only two pieces of data are
required: the domain name and the service name. etcd is used for persistence so
that all instances of strowger get the same configuration.

### Benefits over HAProxy/nginx

The primary benefits are that it uses service discovery natively and supports
dynamic configuration. Both HAProxy and nginx require a new process to be
spawned to change the majority of their configuration.

Since this is very much an alpha prototype, a service discovery shim for HAProxy
would make more sense for production currently.

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
