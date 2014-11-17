---
title: "Flynn Progress: Week Ending Nov. 17, 2014"
date: November 17, 2014
---

We smoothed out many rough edges in Flynn last week by removing some upstream dependencies. You'll see improvements to our security and stability, along with better support for GitHub releases.

## Changes

### Enhancements

- We [updated to Go 1.4rc1](https://github.com/flynn/flynn/pull/468) to improve reliability and security. This may also improve performance as development continues.
- The router's `go-vhost` package dependency [was replaced](https://github.com/flynn/flynn/pull/415) with the new `crypto/tls` functionality in Go 1.4.
- All Go [subrepo paths](https://groups.google.com/forum/#!topic/golang-announce/eD8dh3T9yyA) have been updated. ([#416](https://github.com/flynn/flynn/pull/416))
- Vagrant VMs now use IPs from [RFC 5737](http://tools.ietf.org/html/rfc5737)
  for host communication to work around DNS rebinding protection and avoid
  collisions. ([#438](https://github.com/flynn/flynn/pull/438))
- We added a `version` command to `flynn-host` that will show the release version or git
  revision that it was built from. ([#308](https://github.com/flynn/flynn/pull/308))
- [We added a Makefile](https://github.com/flynn/flynn/pull/435), so the `make` command should be used instead of calling `tup` directly. The `make clean` target replaces the `clean` shell alias.
- [Releases are now tagged](https://github.com/flynn/flynn/pull/440) on GitHub.
- The `flynn-release download` command was moved to the `flynn-host`
  binary, and the `flynn-release` binary was removed from binary packages. ([#446](https://github.com/flynn/flynn/pull/446))
- [Flynn CI](https://ci.flynn.io) now tests all supported buildpacks. ([#417](https://github.com/flynn/flynn/pull/417))
- Bootstrap now supports [waiting for TCP services](https://github.com/flynn/flynn/pull/417).
- The `flynn-bootstrap` command was renamed to `flynn-host bootstrap` and the `flynn-bootstrap` binary was removed. ([#449](https://github.com/flynn/flynn/pull/449))

### Bugfixes

- Releases invalidate the correct CloudFront distribution. ([#451](https://github.com/flynn/flynn/pull/451))
- We deleted our unmaintained Docker backend for the host daemon, closing out four Flynn issues. Support for Docker images remains a key feature of Flynn. ([#430](https://github.com/flynn/flynn/pull/430))

## What's Next

We expect to have nightly builds available shortly. We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Email us](mailto:contact@flynn.io) whatever is on your mind!
