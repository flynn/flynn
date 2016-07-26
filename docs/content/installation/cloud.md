---
title: Cloud Installation
layout: docs
---

# Cloud Installation

The CLI includes a local browser-based installer that can boot and configure
a Flynn cluster on Amazon Web Services, Digital Ocean, Azure, and your own
servers via SSH. It automatically performs all of the steps required to install
Flynn.

Before starting, you need to install the Flynn command-line interface on your
local machine.

On OS X and Linux, run this command in a terminal:

```text
L=/usr/local/bin/flynn && curl -sSL -A "`uname -sp`" https://dl.flynn.io/cli | zcat >$L && chmod +x $L
```

On Windows, run this command in PowerShell:

```text
(New-Object Net.WebClient).DownloadString('https://dl.flynn.io/cli.ps1') | iex
```

## Install

Run `flynn install` on your local machine to start the installer. It will open
in your web browser and walk you through installing Flynn.

After installation, you will be walked through installing the CA certificate to
access the dashboard and automatically logged in. If you need to log in again,
you can [retrieve the login token](/docs/dashboard#login-token) with the CLI.

The CLI is automatically configured with credentials to access the deployed
cluster.

## SSH Access

If you need to access cloud instances provisioned with the installer, you can
connect to them with SSH. The installer will generate an SSH keypair, and will
install the public key on all hosts it provisions. The keys are stored in
`~/.flynn/installer/keys`. The default key is called `flynn`.

To login to a server provisioned by Flynn, you can specify the generated key and
IP address of the server:

```text
ssh -i ~/.flynn/installer/keys/flynn ubuntu@SERVER_IP
```
