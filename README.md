# Flynn Dev Environment

**Note: This repo is broken while we finish bootstrapping Flynn services in containers**

This repo contains a Vagrantfile/Makefile combo that sets up all of the Flynn
components and dependencies in a working dev/test configuration.

The only requirement is that you have [VirtualBox](https://www.virtualbox.org/)
and [Vagrant](http://www.vagrantup.com/) installed.

**Note:** Flynn is alpha-quality software, so things are probably broken.

### Demo video

[![Flynn Demo](https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.png)](https://s3.amazonaws.com/flynn-media/flynn_demo_2013-11-14.mp4)

### Setup

After checking out this repo, boot up the VM in Vagrant:

```text
vagrant up
```

After the VM provisioning has finished, log in to it and run `make` to install
the dependencies and boot up the Flynn services:

```text
vagrant ssh

make
```

### Usage

With the Flynn processes running, open another terminal and deploy the example
application:

```text
vagrant ssh

cd nodejs-example

git push flynn master
```

If the deploy is successful, the example application should have one instance
running which will be running a HTTP server:

```text
curl http://10.0.2.15:55000
```

The `flynn` command line tool is used to manipulate the application.

#### Scale

To test out the router and scaling, turn up the web processes and add a domain:

```text
flynn scale web=3

flynn domain example.com
```

The application will now be accessible via the router:

```text
curl -H "Host: example.com" localhost:8080
```

Repeated requests to the router should show that the requests are load balanced
across the running processes.

#### Logs

`flynn ps` will show the running processes. To get the logs from a process, use
`flynn logs`:

```text
flynn logs web.1
```

#### Run

An interactive one-off process may be spawned in a container:

```text
flynn run bash
```

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
