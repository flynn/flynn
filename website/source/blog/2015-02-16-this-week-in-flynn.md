---
title: "This Week in Flynn"
date: February 16, 2015
---

It's been a busy week at Flynn. Most importantly, Flynn binaries and container
images are now distributed securely using faster servers. We also created an
script that replaces the manual installation instructions.

## Changes

### Enhancements

- [The Update Framework](http://theupdateframework.com/) is now used for secure
  artifact distribution ([#974](https://github.com/flynn/flynn/pull/974),
  [#980](https://github.com/flynn/flynn/pull/980)). All binaries and container
  images are cryptographically signed. Also, container images are now hosted on
  a much faster CDN. We removed the debian package in favor of an installation
  script that allows us to have a distro-agnostic install process in the future.
- The container manager is now capable of registering with service discovery on
  behalf of a process ([#933](https://github.com/flynn/flynn/pull/933)). In
  addition to basic registration, it also does HTTP and TCP health checks and
  can unregister or kill unhealthy processes. The `sdutil` binary was removed as
  this functionality supersedes it.
- The `PGHOST` environment variable is now added when a postgres database is
  provisioned ([#987](https://github.com/flynn/flynn/pull/987)). This allows
  apps to connect to postgres without using the service discovery protocol.
- The HTTP/TCP router now stores routes in Postgres instead of etcd
  ([#959](https://github.com/flynn/flynn/pull/959)). This was the only service
  that stored persistent data in etcd, leaving discoverd as the only etcd
  client.
- Minor improvements were added to deployer logging and cleanup ([#983](https://github.com/flynn/flynn/pull/983))
- Router and other layer 0 bootstrap jobs allowed to use discoverd DNS ([#985](https://github.com/flynn/flynn/pull/985))
- Removed unused port allocator from host daemon ([#991](https://github.com/flynn/flynn/pull/995))
- Script usage is printed when too many arguments are provided ([#1006](https://github.com/flynn/flynn/pull/1006))

### Bugfixes

- Simplified the installation docs ([#996](https://github.com/flynn/flynn/pull/996))
- Bare IPv6 resolver addresses are handled in discoverd ([#997](https://github.com/flynn/flynn/pull/997))
- IP address allocations are tracked during host daemon state restoration ([#990](https://github.com/flynn/flynn/pull/990))
- Cron is disabled before provisioning VM images ([#959](https://github.com/flynn/flynn/pull/959))
- Deploys are only rolled back if jobs fail to start ([#1005](https://github.com/flynn/flynn/pull/1005))

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode

## How You Can Help

* Report a [bug on GitHub](https://github.com/flynn/flynn/issues/new)
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
