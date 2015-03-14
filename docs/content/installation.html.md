---
title: Installation
layout: docs
---

# Installation

Before starting, you should install the Flynn command-line interface by running
this command:

```bash
L=/usr/local/bin/flynn && curl -sL -A "`uname -sp`" https://dl.flynn.io/cli | zcat >$L && chmod +x $L
```

If you want to run Flynn on your local machine, the easiest way is to install the
[Vagrant demo environment](#vagrant).

If you'd like to deploy Flynn to AWS, [you can use the CLI](#aws).

If you want to manually install Flynn, follow the [Ubuntu 14.04 amd64](#ubuntu-14.04-amd64) guide.
Currently only Ubuntu 14.04 amd64 is supported, but this is a temporary packaging limitation, we
have no actual dependency on Ubuntu.

## Vagrant

### Dependencies

Both Vagrant and VirtualBox need to be installed, so if you don't have them already you should
install them by following the directions on their respective web sites:

* [VirtualBox](https://www.virtualbox.org/)
* [Vagrant 1.6 or greater](http://www.vagrantup.com/)

### Installation

Clone the Flynn git repository:

```
$ git clone https://github.com/flynn/flynn
```

Change to the `demo` directory and bring up the Vagrant box:

```
$ cd flynn/demo
$ vagrant up
```

If the VM fails to boot for any reason, you can restart the process by running the following:

```
$ vagrant reload
```

With a successful installation, you will have a single node Flynn cluster running inside the VM,
and the final log line contains a `flynn cluster add` command. Paste that line from the console
output into your terminal and execute it.

Now you have Flynn installed and running, head over to the [Using Flynn](/docs)
page for guides on deploying your applications to Flynn.


## AWS

The [Flynn CLI](https://cli.flynn.io) includes an installer that will boot and
configure a Flynn cluster on Amazon Web Services using CloudFormation. It
automatically performs all of the steps required to install Flynn.

Just run `flynn install` to start the installation.


## Ubuntu 14.04 amd64

Before we get going with the installation, please note that if you plan on running a multi-node
cluster, you should boot at least 3 nodes to keep etcd efficient
(see [here](https://github.com/coreos/etcd/blob/v0.4.6/Documentation/optimal-cluster-size.md) for
an explanation).

*NOTE: If you are installing on Linode, you need to use native kernels (rather than
Linode kernels) for AUFS support, see [this guide](https://www.linode.com/docs/tools-reference/custom-kernels-distros/run-a-distributionsupplied-kernel-with-pvgrub)
for instructions on how to switch.*

### Installation

Download and run the Flynn installer script:

```
$ sudo bash < <(curl -fsSL https://dl.flynn.io/install-flynn)
```

If you would rather take a look at the contents of the script before running it as root, download and
run it in two separate steps:

```
$ curl -fsSL -o /tmp/install-flynn https://dl.flynn.io/install-flynn
... take a look at the contents of /tmp/install-flynn ...
$ sudo bash /tmp/install-flynn
```

Running the installer script will:

1. Install Flynn's runtime dependencies
2. Download, verify and install the `flynn-host` binary
3. Download and verify filesystem images for each of the Flynn components
4. Install an Upstart job for controlling the `flynn-host` daemon

Some of the filesystem images are quite large (hundreds of MB) so step 3 could take a while depending on
your internet connection.

### Rinse and repeat

You should install Flynn as above on every host that you want to be in the Flynn cluster.

### Start Flynn Layer 0

First, ensure that all network traffic is allowed between all nodes in the cluster (specifically
all UDP and TCP packets). The following ports should also be open externally on the firewalls
for all nodes in the cluster:

* 80 (HTTP)
* 443 (HTTPS)
* 2222 (Git over SSH)
* 3000 to 3500 (user defined TCP services)

The next step is to configure a Layer 0 cluster by starting the flynn-host daemon on all
nodes. The daemon uses etcd for leader election, and etcd needs to be aware of all of the
other nodes for it to function correctly.

If you are starting more than one node, the etcd cluster should be configured
using a [discovery
token](https://coreos.com/docs/cluster-management/setup/etcd-cluster-discovery/).
`flynn-host init` is a tool that handles generating and configuring the token.

On the first node, create a new token with the `--init-discovery=3` flag,
replacing `3` with the total number of nodes that will be started. The minimum
multi-node cluster size is three, and this command does not need to be run if
you are only starting a single node.

```
$ sudo flynn-host init --init-discovery=3
https://discovery.etcd.io/ac4581ec13a1d4baee9f9c78cf06a8c0
```

On the rest of the nodes, configure the generated discovery token:

```
$ sudo flynn-host init --discovery https://discovery.etcd.io/ac4581ec13a1d4baee9f9c78cf06a8c0
```

**Note:** a new token must be used every time you restart all nodes in the
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
log file (`/var/log/upstart/flynn-host.log`) for any errors and try starting the daemon
again.

### Start Flynn Layer 1

After you have a running Layer 0 cluster, you can now bootstrap Layer 1 with
`flynn-host bootstrap`. You'll need a domain name with DNS A records pointing to
every node IP address and a second, wildcard domain CNAME to the cluster domain.

**Example**

```
demo.localflynn.com.    A      192.168.84.42
demo.localflynn.com.    A      192.168.84.43
demo.localflynn.com.    A      192.168.84.44
*.demo.localflynn.com.  CNAME  demo.localflynn.com.
```

*If you are just using a single node and don't want to initially setup DNS
records, you can use [xip.io](http://xip.io) which provides wildcard DNS for
any IP address.*

Set `CLUSTER_DOMAIN` to the main domain name and start the bootstrap process,
specifying the number of hosts that are expected to be present.

```
$ sudo \
    CLUSTER_DOMAIN=demo.localflynn.com \
    flynn-host bootstrap --min-hosts=3 /etc/flynn/bootstrap-manifest.json
```

*Note: You only need to run this on a single node in the cluster. It will
schedule jobs on nodes across the cluster as required.*

The Layer 1 bootstrapper will get all necessary services running using the Layer
0 API. The final log line will contain configuration that may be used with the
[command-line interface](/docs/cli).

If you try these instructions and run into issues, please open an issue or pull
request.

Now you have Flynn installed and running, head over to the [Using Flynn](/docs)
page for guides on deploying your applications to Flynn.
