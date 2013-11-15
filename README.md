# Flynn Dev Environment

This repo contains a Vagrantfile/Makefile combo that set up all of the Flynn
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
vagrant reload # required to reboot after installing new kernel
```

If you are using VirtualBox > 4.2.0 you will probably need to update the Guest
Additions with [vagrant-vbguest](https://github.com/dotless-de/vagrant-vbguest):

```
vagrant up
vagrant plugin install vagrant-vbguest
vagrant vbguest
vagrant reload
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
