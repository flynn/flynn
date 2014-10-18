---
title: Installation
layout: docs
---

# Installation

If you want to run Flynn on your local machine, the easiest way is to install the
[Vagrant demo environment](#vagrant).

If you want to manually install Flynn, follow the [Ubuntu 14.04 amd64](#ubuntu-1404-amd64) guide.
Currently *only* Ubuntu 14.04 amd64 is supported, but this is a temporary packaging limitation. Flynn
has no actual dependency on Ubuntu.

## Vagrant

### Dependencies

Both Vagrant and VirtualBox need to be installed, so if you don't have them already you should
install them by following the directions on their respective web sites:

* [VirtualBox](https://www.virtualbox.org/)
* [Vagrant 1.6 or greater](http://www.vagrantup.com/)

The [XZ Utils](http://tukaani.org/xz/) package is required to unpackage Vagrant boxes, which is
available on:

* OS X via [Homebrew](http://brew.sh) with `brew install xz`
* Ubuntu via `apt-get install xz-utils`

You should also download and install the Flynn [Command Line Tools](/docs/cli) by running this command:

```bash
L=/usr/local/bin/flynn && curl -sL -A "`uname -sp`" https://cli.flynn.io/flynn.gz | zcat >$L && chmod +x $L
```

### Demo Installation

Clone the Flynn git repository:

```
$ git clone https://github.com/flynn/flynn
```

Change to the `demo` directory and bring up the Vagrant box:

```
$ cd flynn/demo
$ vagrant up
```

If this is the first time you’re creating the VM, Vagrant will need to download the
underlying VirtualBox files which are ~1GB in size, so this could take several minutes depending on the speed of
your internet connection.

If the VM fails to boot for any reason, you can restart the process by running the following:

```
$ vagrant reload
```

With a successful installation, you will have a single node Flynn cluster running inside the VM,
and the final log line contains a `flynn cluster add` command. Paste that line from the console
output into your terminal and execute it.

Now you have Flynn installed and running, head over to the [Using Flynn](/docs/using-flynn) page for
guides on deploying your applications to Flynn.

## Ubuntu 14.04 amd64

Before we get going with the installation, please note that if you plan on running a multi-node
cluster, you should boot at least 3 nodes to keep `etcd` efficient
(see [the CoreOS GitHub page](https://github.com/coreos/etcd/blob/v0.4.6/Documentation/optimal-cluster-size.md) for
an explanation).

### Dependencies

Flynn uses Docker images to store job filesystems, and by default uses the AUFS filesystem driver. To
check if your system supports AUFS, run this command:

```
$ sudo modprobe aufs
```

If you get no output, then AUFS is supported, but if you get `modprobe: FATAL: Module aufs not found.`
then you need to run the following commands:

```
$ sudo apt-get update
$ sudo apt-get install linux-image-extra-$(uname -r)
```

### Installation

Flynn is available as a Debian package from our `apt` repository.

First, add the Flynn repository key to your list of trusted `apt` keys:

```
$ sudo apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv BC79739C507A9B53BB1B0E7D820A5489998D827B
```

Now add the Flynn repository to your `apt` sources list and install the `flynn-host` package:

```
$ echo deb https://dl.flynn.io/ubuntu flynn main | sudo tee /etc/apt/sources.list.d/flynn.list
$ sudo apt-get update
$ sudo apt-get install flynn-host
```

### Download Images

Flynn is made up of many interacting components, each of which get built into a Docker image and pushed
to the public Docker registry.

Before you can run Flynn, you will need to download these images by running the following:

```
$ sudo flynn-release download /etc/flynn/version.json
```

Some of the images are quite large (hundreds of MB) so this could take a while depending on
your internet connection.

### Rinse and Repeat

Follow the instructions above on every host that you want to add to the Flynn cluster.

### Start Flynn Layer 0

First, ensure that the following ports are open externally on the firewalls for all
nodes in the cluster:

* **80** *HTTP*
* **443** *HTTPS*
* **2222** *Git over SSH*
* **3000 to 3500** *User-Defined TCP Services*

*The nodes also need to be able to communicate with each other internally on all ports.*

The next step is to configure a Layer 0 cluster by starting the `flynn-host` daemon on all
nodes. The daemon uses `etcd` for leader election, and `etcd` needs to be aware of all of the
other nodes for it to function correctly.

If you are starting more than one node, the `etcd` cluster should be configured
using a [discovery
token](https://coreos.com/docs/cluster-management/setup/etcd-cluster-discovery/).
Get a token [from here](https://discovery.etcd.io/new) and add a line like this
to the Flynn Upstart file `/etc/init/flynn-host.conf` on every node:

```text
env ETCD_DISCOVERY=https://discovery.etcd.io/00000000000000000000000000000000
```

**Note:** A new token must be used every time you restart all nodes in the
cluster.

Then, start the daemon by running:

```
$ sudo start flynn-host
```

You can check the status of the daemon by running:

```
$ sudo status flynn-host
flynn-host start/running, process 4090
```

If the status is `stop/waiting`, the daemon has failed to start for some reason. Check the
log file `/var/log/upstart/flynn-host.log` for any errors and try starting the daemon
again.

### Start Flynn Layer 1

After you have a running Layer 0 cluster, you can now bootstrap Layer 1 with
`flynn-bootstrap`. You'll need a domain name with DNS A records pointing to
every node IP address and a second, wildcard domain CNAME to the cluster domain.

**Example**

```
demo.localflynn.com.    A      192.168.84.42
demo.localflynn.com.    A      192.168.84.43
demo.localflynn.com.    A      192.168.84.44
*.demo.localflynn.com.  CNAME  demo.localflynn.com.
```

Set `CONTROLLER_DOMAIN` and `DEFAULT_ROUTE_DOMAIN` to the main domain name and
start the bootstrap process:

```
$ sudo \
    CONTROLLER_DOMAIN=demo.localflynn.com \
    DEFAULT_ROUTE_DOMAIN=demo.localflynn.com \
    flynn-bootstrap /etc/flynn/bootstrap-manifest.json
```

The Layer 1 bootstrapper will get all necessary services running using the Layer
0 API. The final log line will contain configuration that may be used with the
[command-line interface](/docs/cli).

## Welcome to Flynn!

Now that you have Flynn installed and running, head over to the [Using Flynn](/docs/using-flynn)
page for guides on deploying your applications to Flynn.

If you try these instructions and run into issues, please [open an issue](https://github.com/flynn/flynn/issues/new) or [pull
request](https://github.com/flynn/flynn/compare).
