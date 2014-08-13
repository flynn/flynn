---
title: Flynn Beta
date: August 13, 2014
---

Flynn is now in beta.

This release represents over a year of design and development and is the last
stage before production release candidates begin shipping.

The first beta release includes:

- a **pluggable containerization backend** with support for Red Hat's
  libvirt-lxc and Docker 1.1
- **deterministic releases** using image IDs (currently libvirt only)
- **flap detection** in the controller scheduler
- **multi-node support**
- support for **etcd v0.4.6**
- support for **Ubuntu 14.04 LTS** including an updated slug base image
- **deployment instructions**
- and numerous **bug fixes** for all major components.

This release also ships as a single repo for the first time. Components are
still modular, but everything is located at
[github.com/flynn/flynn](https://github.com/flynn/flynn).

Now is an excellent time to set up a Flynn cluster for development, testing, and
staging environments. We need your help to find bugs and discover edge cases so
we can expand the test suite and improve the system.

Development is still in a feature freeze until the planned production-ready
release this fall. With the exception of the appliance framework, no new
features will be added until the first production stability release candidate.

Thank you for your continued support and enthusiasm.

— The Flynn Team
