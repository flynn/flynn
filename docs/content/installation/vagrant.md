---
title: Vagrant Installation
layout: docs
---

# Vagrant Installation

Vagrant is a virtual machine manager that runs on your local machine. We provide
a pre-built Vagrant/VirtualBox VM image that can be used to try out Flynn
locally without manually installing it.

## Dependencies

Both Vagrant and VirtualBox need to be installed, so if you don't have them already you should
install them by following the directions on their respective web sites:

* [VirtualBox](https://www.virtualbox.org/wiki/Downloads)
* [Vagrant 1.6 or greater](https://www.vagrantup.com/downloads.html)

## Installation

Clone the Flynn git repository:

```
$ git clone https://github.com/flynn/flynn
```

A Makefile is provided for convenience as a simple wrapper around common flynn
commands. For systems without `make` installed, manually run the `vagrant` commands.

Change to the `demo` directory and bring up the Vagrant box:

```
$ cd flynn/demo

# Provision the VM and bootstrap a flynn cluster
# Init should only be called once
$ make init
# Print login token and open dashboard in browser
$ make dashboard

# OR

$ vagrant up
# Follow the instructions output by vagrant up, then...
$ flynn -a dashboard env get LOGIN_TOKEN
# Copy the login token
# Open the dashboard in a browser
$ open http://dashboard.demo.localflynn.com
```

Additional make commands:

```
# Halt the VM
$ make down

# Bring up the VM and flynn
$ make up

# SSH into the VM
$ make ssh

# Destroy the VM
$ make destroy

# See Makefile for other useful commands
```

If the VM fails to boot for any reason, you can restart the process by running the following:

```
# Stop and restart the VM
$ make down
$ make up

# OR, destroy and rebuild the VM
$ make reset
```

With a successful installation, you will have a single node Flynn cluster running inside the VM.

Now you have Flynn installed and running, head over to the [Flynn
Basics](/docs/basics) page for a tutorial on deploying an application to Flynn.
