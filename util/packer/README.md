# Packer Templates

This directory contains Packer templates for building machine images that
represent an Ubuntu target system for Flynn. This is essentially a stock image
of Ubuntu 14.04 with Docker installed and the Flynn container images downloaded.

## Usage

First, [install Packer](http://www.packer.io/intro/getting-started/setup.html).
Then, clone this repository and `cd` into the `util/packer/ubuntu-14.04` target
directory.

## Vagrant Template

Currently supports:
 * VirtualBox
 * VMWare Fusion

To build just a VirtualBox image for use with the Flynn Vagrantfile, first
download an Ubuntu cloud image and convert it to an OVA archive:

```
$ BOX_URL=https://cloud-images.ubuntu.com/vagrant/trusty/current/trusty-server-cloudimg-amd64-vagrant-disk1.box
$ curl $BOX_URL | tar --delete Vagrantfile > ubuntu.ova
```

Then run Packer:

```
$ packer build -only=virtualbox-ovf template.json
```

## Then What?

At the end of any of these, you'll have a snapshot or image ready to go.
