---
title: "This Week in Flynn"
date: December 29, 2014
---

We pushed many bug fixes last week, aided by the Go race detector. Running that tool against our Continuous Integration system helped fix many small races and leaks.

We've had several requests to add buildpack caching to Flynn, and that change landed last week. Regular deploys of your apps should run much more quickly now.

Thanks to some improvements in etcd 2.0rc1, we've also added `flynn-host init`, which makes configuring clusters more reliable.

### Enhancements

- Updated etcd to v2.0rc1 ([#514](https://github.com/flynn/flynn/pull/514))
- Added `flynn-host init` command for configuring etcd clusters ([#514](https://github.com/flynn/flynn/pull/514))
- Added a single, persistent SSH connection for CI streams ([#640](https://github.com/flynn/flynn/pull/640))
- Implemented buildpack build caching ([#653](https://github.com/flynn/flynn/pull/653))
- Now `exec` target process in slugrunner without forking first ([#655](https://github.com/flynn/flynn/pull/655))
- Added test harness for running multiple commands inside a single job ([#658](https://github.com/flynn/flynn/pull/658))
- Updated Python, Java, Scala, and PHP buildpacks ([#662](https://github.com/flynn/flynn/pull/662))
- Bump CI VM memory to 2GB ([#667](https://github.com/flynn/flynn/pull/667))
- Added fallback log-retrieval mode to CI ([#669](https://github.com/flynn/flynn/pull/669))

### Bugfixes

- No longer leak iptables rules on CI ([#370](https://github.com/flynn/flynn/pull/370))
- Using HTTP Basic Authentication when downloading Docker images ([#630](https://github.com/flynn/flynn/pull/630))
- Fixed race in image layer downloads ([#645](https://github.com/flynn/flynn/pull/645))
- Fixed race in port allocation ([#645](https://github.com/flynn/flynn/pull/645))
- Fixed race in CI log handling ([#645](https://github.com/flynn/flynn/pull/645))
- Fixed race in image processing ([#646](https://github.com/flynn/flynn/pull/646))
- Fixed multiple races and leaks in log handling ([#649](https://github.com/flynn/flynn/pull/649))
- Fixed attach failure path when there are no job logs ([#650](https://github.com/flynn/flynn/pull/650))
- Fixed `pkg/exec` when no streams are attached ([#650](https://github.com/flynn/flynn/pull/650))
- Fixed race during postgres bootstrap ([#659](https://github.com/flynn/flynn/pull/659))
- Fixed race in controller restart test ([#660](https://github.com/flynn/flynn/pull/660))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
