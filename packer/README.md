# Packer Templates

This directory contains Packer templates for building machine images that
represent an Ubuntu target system for Flynn. This is essentially a stock image
of Ubuntu 14.04 with Docker installed and the Flynn container images downloaded.

## Usage

First, [install Packer](http://www.packer.io/intro/getting-started/setup.html).
Then, clone this repository and `cd` into the `packer/ubuntu-14.04` target
directory.

## Vagrant Template

Currently supports:
 * Virtualbox
 * VMWare Fusion

To build just a Virtualbox image for use with the Flynn Vagrant file:

```
$ packer build -only=virtualbox-iso vagrant.json
```

## Then What?

At the end of any of these, you'll have a snapshot or image ready to go.
