---
title: "This Week in Flynn"
date: February 9, 2015
---

We made a lot of progress this week. We improved the router by adding support for early disconnect detection and timeouts for slow clients. We also added service metadata to discoverd, improving Flynn's ability to support database appliances.

We also refactored the controller, router and volume management components for better clarity and error handling.

## Enhancements

- Refactored SSE handling into generic helpers ([#899](https://github.com/flynn/flynn/pull/899))
- Added service metadata to discoverd ([#919](https://github.com/flynn/flynn/pull/919))
- Refactored volume manager into a separate package ([#932](https://github.com/flynn/flynn/pull/932))
- Refactored test utilities into a package ([#935](https://github.com/flynn/flynn/pull/935))
- Added pagination to blog ([#936](https://github.com/flynn/flynn/pull/936))
- Preliminary work for volume persistence ([#940](https://github.com/flynn/flynn/pull/940))
- Added taffy integration test ([#946](https://github.com/flynn/flynn/pull/946))
- Added flag to blobstore to disable service discovery ([#947](https://github.com/flynn/flynn/pull/947))
- Added support to CI for booting clean VMs ([#948](https://github.com/flynn/flynn/pull/948))
- Added `tuf` binary to CI rootfs ([#951](https://github.com/flynn/flynn/pull/951))
- Added context passing to router ([#955](https://github.com/flynn/flynn/pull/955))
- Refactored router's upgrade status code handling ([#960](https://github.com/flynn/flynn/pull/960))
- Bumped Ubuntu version in base images ([#962](https://github.com/flynn/flynn/pull/962))
- Added detection of early termination of downstream TCP connections in router ([#964](https://github.com/flynn/flynn/pull/964))
- Removed fake release artifacts for dashboard ([#969](https://github.com/flynn/flynn/pull/969))

## Bugfixes

- Fixed CLI error output ([#939](https://github.com/flynn/flynn/pull/939))
- Fixed removing VirtualBox packages ([#941](https://github.com/flynn/flynn/pull/941))
- Return hijack errors when doing connection upgrades in router ([#950](https://github.com/flynn/flynn/pull/950))
- Return an "unknown" exit code in attach client ([#952](https://github.com/flynn/flynn/pull/952))
- Bumped TOML package ([#957](https://github.com/flynn/flynn/pull/957))
- Fixed deployments with zero processes ([#961](https://github.com/flynn/flynn/pull/961))
- Fixed `flynn create --yes` ([#965](https://github.com/flynn/flynn/pull/965))
- Made ZFS installation instructions consistent ([#968](https://github.com/flynn/flynn/pull/968))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)