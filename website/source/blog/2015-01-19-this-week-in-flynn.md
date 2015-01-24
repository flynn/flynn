---
title: "This Week in Flynn"
date: January 19, 2015
---

Many improvements landed in Flynn this week. App deployment logs are now exposed in the dashboard even after a deploy has finished.

We also converted the host service and scheduling framework APIs to HTTP. We will continue to migrate all other rpcplus APIs to HTTP in the future.

## Enhancements

- Migrated host service and scheduling framework from rpcplus to HTTP API ([#663](https://github.com/flynn/flynn/pull/663), [#728](https://github.com/flynn/flynn/pull/728), [#733](https://github.com/flynn/flynn/pull/733), [#729](https://github.com/flynn/flynn/pull/729))
- Exposed job metadata from controller ([#730](https://github.com/flynn/flynn/pull/730))
- Refactored server-sent events writer ([#731](https://github.com/flynn/flynn/pull/731))
- Exposed historical app deploy logs in dashboard ([#735](https://github.com/flynn/flynn/pull/735))
- Added OpenJDK 7 and PostgreSQL headers to cedarish base image ([#740](https://github.com/flynn/flynn/pull/740))
- Added log15 logging to scheduling framework ([#745](https://github.com/flynn/flynn/pull/745))
- Bumped to Go v1.4.1 ([#747](https://github.com/flynn/flynn/pull/747))
- Refactored controller helper to use httprouter instead of martini ([#748](https://github.com/flynn/flynn/pull/748))
- Added standardized JSON error response type ([#749](https://github.com/flynn/flynn/pull/749))
- Deprecated `make dev` in favor of `make` ([#751](https://github.com/flynn/flynn/pull/751))
- Added make targets for running tests ([#757](https://github.com/flynn/flynn/pull/757))
- Passed config to containerinit using JSON ([#760](https://github.com/flynn/flynn/pull/760))

## Bugfixes

- Fixed unit tests in development VM ([#689](https://github.com/flynn/flynn/pull/689))
- Switched to RFC 6598 shared address space for overlay network to avoid collisions with EC2 DNS ([#724](https://github.com/flynn/flynn/pull/724))
- httpclient no longer leaks response bodies ([#732](https://github.com/flynn/flynn/pull/732))
- Fixed `go vet` complaints in tests ([#739](https://github.com/flynn/flynn/pull/739))
- Don't remove custom vagrant config in `make clean` ([#741](https://github.com/flynn/flynn/pull/741))
- Fixed panic in CI runner when setting GitHub status ([#742](https://github.com/flynn/flynn/pull/742))
- Limited concurrent CI builds to 3 ([#743](https://github.com/flynn/flynn/pull/743))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)