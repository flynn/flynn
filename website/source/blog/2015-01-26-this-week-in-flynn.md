---
title: "This Week in Flynn"
date: January 26, 2015
---

We shipped a number of significant updates to core Flynn services this week, including a complete rewrite of service discovery. The new [discoverd](https://github.com/flynn/flynn/tree/master/discoverd) includes an HTTP API and exposes services using DNS.

As part of a larger upgrade to the HTTP router, we switched to the standard Go HTTP package. Flynn's unit tests now run directly in Flynn CI. The Flynn AMIs are now available in all EC2 regions.

## Enhancements

- Rewrote [discoverd](https://github.com/flynn/flynn/tree/master/discoverd) to support HTTP and DNS ([#807](https://github.com/flynn/flynn/pull/807), [#811](https://github.com/flynn/flynn/pull/811), [#812](https://github.com/flynn/flynn/pull/812), [#814](https://github.com/flynn/flynn/pull/814))
- Refactored HTTP routing to use `net/http` and support backend keep-alive and HTTP/1.0 ([#781](https://github.com/flynn/flynn/pull/781), [#794](https://github.com/flynn/flynn/pull/794), [#798](https://github.com/flynn/flynn/pull/798))
- Refactored [controller](https://github.com/flynn/flynn/tree/master/controller) to use httprouter instead of martini ([#762](https://github.com/flynn/flynn/pull/762))
- Refactored controller tests to use controller client ([#806](https://github.com/flynn/flynn/pull/806))
- Refactored attach protocol for better HTTP compliance ([#761](https://github.com/flynn/flynn/pull/761), [#793](https://github.com/flynn/flynn/pull/793), [#800](https://github.com/flynn/flynn/pull/800))
- Gracefully handle [discoverd](https://github.com/flynn/flynn/tree/master/discoverd) unregistration during daemon shutdown ([#819](https://github.com/flynn/flynn/pull/819))
- Running unit tests in Flynn CI instead of Travis ([#792](https://github.com/flynn/flynn/pull/792), [#801](https://github.com/flynn/flynn/pull/801))
- Now providing JSON error responses in HTTP API client ([#765](https://github.com/flynn/flynn/pull/765))
- Allowed custom `git remote` when creating/deleting apps in CLI ([#777](https://github.com/flynn/flynn/pull/777))
- Created bridge directly instead of via libvirt ([#785](https://github.com/flynn/flynn/pull/785), [#787](https://github.com/flynn/flynn/pull/787))
- Improved router tests ([#782](https://github.com/flynn/flynn/pull/782), [#788](https://github.com/flynn/flynn/pull/788))
- Added support for falling back to default keypair in router ([#769](https://github.com/flynn/flynn/pull/769))
- Deleted unmaintained MongoDB appliance prototype ([#768](https://github.com/flynn/flynn/pull/768))
- Refactored postgres helpers ([#786](https://github.com/flynn/flynn/pull/786))
- Log errors when dumping CI logs ([#813](https://github.com/flynn/flynn/pull/813))
- Copied AMI to all EC2 regions ([#818](https://github.com/flynn/flynn/pull/818))
- Removed extraneous dial fields from HTTP API client ([#815](https://github.com/flynn/flynn/pull/815), [#820](https://github.com/flynn/flynn/pull/820))

## Bugfixes

- Cancelling job restart timers when scaling down ([#610](https://github.com/flynn/flynn/pull/610))
- Fixed DCO link in commit validator ([#770](https://github.com/flynn/flynn/pull/770))
- Disabled broken postgres follower mode ([#772](https://github.com/flynn/flynn/pull/772), [#776](https://github.com/flynn/flynn/pull/776))
- Removed broken router test mode ([#775](https://github.com/flynn/flynn/pull/775))
- Made domain lookup case-insensitive in router ([#778](https://github.com/flynn/flynn/pull/778))
- Improved CI rootfs build error handling ([#783](https://github.com/flynn/flynn/pull/783))
- Added missing error check in controller database query ([#790](https://github.com/flynn/flynn/pull/790))
- Fixed panic when flannel fails to start ([#795](https://github.com/flynn/flynn/pull/795))
- Fixed race when starting router ([#797](https://github.com/flynn/flynn/pull/797))
- Avoided races in shutdown handler ([#803](https://github.com/flynn/flynn/pull/803), [#817](https://github.com/flynn/flynn/pull/817))
- Bumped timeout in an intermittently failing test ([#808](https://github.com/flynn/flynn/pull/808))
- Fixed double channel close in controller ([#828](https://github.com/flynn/flynn/pull/828))
- Don't reuse bridge IPs right away in Flynn CI ([#829](https://github.com/flynn/flynn/pull/829))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)