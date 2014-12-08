---
title: "This Week in Flynn"
date: December 8, 2014
---

We eliminated several races and panics in the continuous integration suite.

We converted the last rpcplus endpoint in the Flynn controller to HTTP. We will migrate all other rpcplus components to HTTP in the future.

## Changes

### Enhancements

- Updated to Go 1.4rc2 ([#550](https://github.com/flynn/flynn/pull/550))
- Added `SO_REUSEPORT` to discoverd and router to allow zero-downtime replacement ([#541](https://github.com/flynn/flynn/pull/541))
- Converted controller's StreamFormations to HTTP from rpcplus ([#540](https://github.com/flynn/flynn/pull/540))
- Merged `pkg/migrate` into `pkg/postgres` ([#556](https://github.com/flynn/flynn/pull/556))
- Moved some database helpers into `pkg/postgres` ([#557](https://github.com/flynn/flynn/pull/557))
- Imported upstream `pq` package improvements ([#560](https://github.com/flynn/flynn/pull/560))
- Added example generator and support packages to router API ([#548](https://github.com/flynn/flynn/pull/548))

### Bugfixes

- Blobstore now registers with discoverd when using the local filesystem adapter ([#543](https://github.com/flynn/flynn/pull/543))
- PATH in Development Vagrantfile is now escaped properly ([#553](https://github.com/flynn/flynn/pull/553))
- Repaired a broken pipe while downloading images ([#529](https://github.com/flynn/flynn/pull/529))
- Calmed a panic after failing to connect to hosts from the controller scheduler ([#562](https://github.com/flynn/flynn/pull/562))
- Ended several races in integration test helpers ([#561](https://github.com/flynn/flynn/pull/561))
- Prevented timeouts when connecting to containers ([#564](https://github.com/flynn/flynn/pull/564))
- Improved job type sorting in `flynn ps` ([#566](https://github.com/flynn/flynn/pull/566))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
