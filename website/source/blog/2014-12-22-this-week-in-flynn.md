---
title: "This Week in Flynn"
date: December 22, 2014
---

We spent a lot of time on the stability of our Continuous Integration System this week.

Several buildpack changes landed, updating Clojure, Python, and Go, and removing support for Grails.

We also deployed some major improvements to the dashboard, adding better GitHub integration for self-hosted Flynn installations and refactoring the user interface.

## Changes

### Enhancements

- Updated buildpacks, including [Clojure Leiningen](http://leiningen.org) 2.5.0, Python 2.7.9, and Go 1.4 ([#605](https://github.com/flynn/flynn/pull/605))
- The CI base image was upgraded to Ubuntu 14.04.1 ([#636](https://github.com/flynn/flynn/pull/636))
- Slow networking in some VirtualBox VMs has been improved ([#589](https://github.com/flynn/flynn/pull/589))
- The unmaintained Grails buildpack has been removed ([#623](https://github.com/flynn/flynn/pull/623))
- The TLS configuration of the router now gets an 'A' grade on [SSL Labs](https://www.ssllabs.com/) ([#629](https://github.com/flynn/flynn/pull/629))
- Self-hosted dashboards now include GitHub token support ([#614](https://github.com/flynn/flynn/pull/614))
- The dashboard layout was refactored ([#637](https://github.com/flynn/flynn/pull/637))
- Now using `atomic.Value` instead of unsafe pointers in sampi ([#638](https://github.com/flynn/flynn/pull/638))
- The scheduling algorithm now uses hosts with the fewest jobs running ([#639](https://github.com/flynn/flynn/pull/639))


### Bugfixes

- Don't use a broken version of Composer in the PHP buildpack ([#616](https://github.com/flynn/flynn/pull/616))
- Fixed taffy image configuration in bootstrap ([#613](https://github.com/flynn/flynn/pull/613))
- Fixed build order for cedarish dependents ([#618](https://github.com/flynn/flynn/pull/618))
- Fixed dropped OS signals in several components ([#624](https://github.com/flynn/flynn/pull/624))
- Fixed double unlock of mutexes in test helpers ([#628](https://github.com/flynn/flynn/pull/628))
- Fixed nil pointer dereference in test ([#633](https://github.com/flynn/flynn/pull/633))
- Fixed races in router integration tests ([#634](https://github.com/flynn/flynn/pull/634))
- Ensured that the bridge MAC does not fluctuate on CI ([#635](https://github.com/flynn/flynn/pull/635))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
