# Packer Templates

This repository contains Packer templates for building machine images
that represent the expected Ubuntu target system for Flynn. This is essentially
a stock image of Ubuntu 12.04 that is Docker-ready, which involves upgrading the
kernel and adding the Docker package repos.

## Usage

First, [install Packer](http://www.packer.io/intro/getting-started/setup.html).
Then, clone this repository and `cd` into the `ubuntu-12.04` target directory.
You can now either build a cloud image with the `cloud.json` or build a Vagrant
box with `vagrant.json`.

## Vagrant Template

Currently supports:
 * Virtualbox
 * VMWare Fusion

To build just a Virtualbox image for use with the Flynn Vagrant file:

```
$ packer build -only=virtualbox-iso vagrant.json
```

## Cloud Template

Currently supports:
 * DigitalOcean
 * EC2 (almost, try it and submit PR)

To build a snapshot for DigitalOcean, run:

```
$ export DIGITALOCEAN_CLIENT_ID="your DO client id"
$ export DIGITALOCEAN_API_KEY="your DO api key"
$ packer build -only digitalocean cloud.json
```

## Then What?

At the end of any of these, you'll have a snapshot or image ready to go.
