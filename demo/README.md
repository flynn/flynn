# Flynn Demo Environment

This repo contains a Vagrantfile that boots up Flynn layer 0 and then bootstraps
Flynn layer 1.
## Prerequisites

* [VirtualBox](https://www.virtualbox.org/)
* [Vagrant 1.6 or greater](http://www.vagrantup.com/)
* [XZ Utils](http://tukaani.org/xz/)
	* XZ is available on OS X via [Homebrew](http://brew.sh) `brew install xz`
	* XZ is available on Ubuntu via `apt-get xz-utils`

### Install Flynn CLI

Download and install our [Command Line Tools](/cli) by running this command:

```bash
L=/usr/local/bin/flynn && curl -sL -A "`uname -sp`" https://cli.flynn.io/flynn.gz | zcat >$L && chmod +x $L
```

### Setup Cluster

Check out this repo, and boot up the VM using Vagrant:

```text
git clone https://github.com/flynn/flynn
cd flynn/demo
vagrant up
```

If you see an error unpackaging the box, first make sure you are running Vagrant
v1.6 or later. You may need to install [XZ](http://tukaani.org/xz/) (see [Prerequisites](#prerequisites)) above.

With a successful installation, The final log line contains a `flynn cluster add` command. Paste that line from the console output into your terminal and execute it.

If you run into a `no such host` error when running the command, verify that
`demo.localflynn.com` resolves to `192.168.84.42` locally. If the domain does
not resolve, your DNS server probably has
[rebinding](https://en.wikipedia.org/wiki/DNS_rebinding) protection on. A quick
workaround is to add this line to your `/etc/hosts` file:

```text
192.168.84.42 demo.localflynn.com example.demo.localflynn.com
```

[See here](https://github.com/flynn/flynn/issues/74#issuecomment-51848061) for
more info.


### Usage

With the Flynn cluster running and the `flynn` tool installed, the first thing you'll
want to do is add your SSH key so that you can deploy applications:

```text
flynn key add
```

After adding your ssh key, you can deploy an application using git. We have a Node.js example for you to try:

```text
git clone https://github.com/flynn/nodejs-flynn-example
cd nodejs-flynn-example
flynn create example
git push flynn master
```

#### Scale

To access the application, add some web processes using the `scale`
command. We'll spin up three processes here:

```text
flynn scale web=3
```

Visit the application [in your browser](http://example.demo.localflynn.com) or with curl:

```text
curl http://example.demo.localflynn.com
```

Repeated requests should show that the requests are load balanced across the
running processes. You can watch the port number change when Flynn directs you to a different process!

```text
Hello from Flynn on port 55007 from container b8ddaba8c0384988bdc4b81e5603f76e
Hello from Flynn on port 55008 from container b8ddaba8c0384988bdc4b81e5603f76e
Hello from Flynn on port 55009 from container b8ddaba8c0384988bdc4b81e5603f76e
Hello from Flynn on port 55007 from container b8ddaba8c0384988bdc4b81e5603f76e
```

#### Logs

`flynn ps` will show the running processes:

```text
$ flynn ps
ID                                             TYPE
e4cffae4ce2b-8cb1212f582f498eaed467fede768d6f  web
e4cffae4ce2b-da9c86b1e9e743f2acd5793b151dcf99  web
e4cffae4ce2b-1b17dd7be8e44ca1a76259a7bca244e1  web
```

To get the log from a process, use `flynn log`:

```text
$ flynn log e4cffae4ce2b-8cb1212f582f498eaed467fede768d6f
Listening on 55007
```

#### Run

An interactive one-off process may be spawned in a container:

```text
flynn run bash
```
