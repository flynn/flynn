---
title: How do I SSH to cluster hosts provisioned by Flynn?
layout: docs
toc_min_level: 2
---

# How do I SSH to cluster hosts provisioned by Flynn?

Flynn's installer will generate an SSH keypair, and will install the public key in the root account on all hosts it provisions. The keys are stored in `~/.flynn/installer/keys`. The default key is called `flynn`.

To login to a server provisioned by Flynn, you can run:

    $ ssh -l root -i ~/.flynn/installer/keys/flynn <ip>
