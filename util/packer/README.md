# Packer Templates

This directory contains Packer templates for building machine images that
represent an Ubuntu target system for Flynn. These are essentially stock images
of Ubuntu 16.04 with Flynn installed.

## Usage

First, [install Packer](http://www.packer.io/intro/getting-started/setup.html).
Then, clone this repository and `cd` into the `util/packer` target directory.

## Vagrant Template

Currently supports:
 * VirtualBox
 * VMWare Fusion

To build just a VirtualBox image for use with the Flynn Vagrantfile:

```
$ packer build -only=virtualbox-iso -var-file ubuntu-xenial.json ubuntu.json
```

## Then What?

At the end of any of these, you'll have a snapshot or image ready to go.
