---
title: Installation
layout: docs
---

# Installation

Before starting, you should install the Flynn command-line interface.

On OS X and Linux, run this command in a terminal:

```text
L=/usr/local/bin/flynn && curl -sSL -A "`uname -sp`" https://dl.flynn.io/cli | zcat >$L && chmod +x $L
```

On Windows, run this command in PowerShell:

```text
(New-Object Net.WebClient).DownloadString('https://dl.flynn.io/cli.ps1') | iex
```

The CLI includes a local browser-based installer that can boot and configure
a Flynn cluster on Amazon Web Services, DigitalOcean, Azure, and your own
servers via SSH. It automatically performs all of the steps required to install
Flynn.

Just run `flynn install` to start the installer.

If you want to run Flynn on your local machine, the easiest way is to install the
[Vagrant demo environment](/docs/installation/vagrant).

If you want to manually install Flynn, follow the [manual
installation](/docs/installation/manual) guide.
