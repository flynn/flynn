---
title: "This Week in Flynn"
date: February 2, 2015
---

Last week we shipped a new version of the controller which includes zero-downtime deploys. When rolling out a new version of an application, requests start going to the new release and the old release will be scaled down without downtime.

We dramatically improved the stability and reliability of Flynn CI, including fixing a networking issue that was causing builds to fail and adding more aggressive timeouts.

We landed the first stages of Flynn's new volume management system backed by ZFS. This feature will be leveraged by Flynn's database appliances in the future.

We also upgraded several buildpacks, etcd, and the version of the Linux kernel installed in our development and testing VMs.

## Enhancements

- Added deployer daemon to perform zero-downtime deploys of apps ([#784](https://github.com/flynn/flynn/pull/784), [#832](https://github.com/flynn/flynn/pull/832), [#836](https://github.com/flynn/flynn/pull/836), [#841](https://github.com/flynn/flynn/pull/841), [#868](https://github.com/flynn/flynn/pull/868), [#873](https://github.com/flynn/flynn/pull/873), [#851](https://github.com/flynn/flynn/pull/851))
- Added JSON schemas for the controller to integration test, generate API docs, and validate requests ([#471](https://github.com/flynn/flynn/pull/471), [#846](https://github.com/flynn/flynn/pull/846), [#903](https://github.com/flynn/flynn/pull/903), [#904](https://github.com/flynn/flynn/pull/904))
- Now handling HTTP upgrades in router according to RFC 7230 ([#826](https://github.com/flynn/flynn/pull/826))
- Releases are no longer required to reference an artifact ([#830](https://github.com/flynn/flynn/pull/830))
- CI will only test pull requests and pushes to master ([#838](https://github.com/flynn/flynn/pull/838))
- Refactored router proxy logic into a separate package ([#842](https://github.com/flynn/flynn/pull/842))
- Added more aggressive timeouts to CI ([#844](https://github.com/flynn/flynn/pull/844))
- Exposed panics during shutdown ([#850](https://github.com/flynn/flynn/pull/850))
- Removed unused HTTPError type in router client ([#852](https://github.com/flynn/flynn/pull/852))
- Refactored controller resource test ([#853](https://github.com/flynn/flynn/pull/853))
- Updated to etcd 2.0 ([#860](https://github.com/flynn/flynn/pull/860))
- Bumped kernel to 3.16 ([#861](https://github.com/flynn/flynn/pull/861))
- Added volume management subsystem with ZFS backend ([#750](https://github.com/flynn/flynn/pull/750), [#866](https://github.com/flynn/flynn/pull/866), [#865](https://github.com/flynn/flynn/pull/865))
- Added additional logging to CLI log tests ([#871](https://github.com/flynn/flynn/pull/871))
- Added test that the router proxies query parameters correctly ([#876](https://github.com/flynn/flynn/pull/876))
- Test suites now use single etcd/discoverd pairs for the router and discoverd tests ([#879](https://github.com/flynn/flynn/pull/879), [#920](https://github.com/flynn/flynn/pull/920))
- Now storing host identifier in host API client ([#880](https://github.com/flynn/flynn/pull/880))
- Added helper method to merge container configurations ([#883](https://github.com/flynn/flynn/pull/883))
- Refactored HTTP context handling ([#889](https://github.com/flynn/flynn/pull/889))
- Added delay to etcd reconnect attempts ([#891](https://github.com/flynn/flynn/pull/891))
- Added HTTP request logging to host daemon ([#894](https://github.com/flynn/flynn/pull/894))
- Redirected CI log URLs to S3 ([#897](https://github.com/flynn/flynn/pull/897))
- Refactored CI cluster management API ([#901](https://github.com/flynn/flynn/pull/901))
- Added optional pprof handler to router ([#909](https://github.com/flynn/flynn/pull/909))
- Now building CI runner binary with race detector on ([#911](https://github.com/flynn/flynn/pull/911))
- Added discoverd health check-based registrar ([#913](https://github.com/flynn/flynn/pull/913))
- Refactored router listener setup ([#915](https://github.com/flynn/flynn/pull/915))
- Added UTF-8 charset to CI logs ([#921](https://github.com/flynn/flynn/pull/921))
- Updated buildpacks to the latest versions ([#922](https://github.com/flynn/flynn/pull/922))
- Added tool for bumping buildpack versions ([#922](https://github.com/flynn/flynn/pull/922))
- Removed pre-installed guest additions from Virtualbox base image ([#923](https://github.com/flynn/flynn/pull/923))


## Bugfixes

- Fixed infinite loop in discoverd ([#831](https://github.com/flynn/flynn/pull/831))
- Fixed intermittently failing discoverd health check test ([#835](https://github.com/flynn/flynn/pull/835))
- Fixed error code constant type declarations ([#839](https://github.com/flynn/flynn/pull/839))
- Fixed consistency issue in resource JSON schema ([#843](https://github.com/flynn/flynn/pull/843))
- Don't assign IP addresses to tap devices on CI ([#844](https://github.com/flynn/flynn/pull/844))
- Fixed CI networking instability ([#857](https://github.com/flynn/flynn/pull/857))
- Don't attempt to copy AMI to unsupported region ([#858](https://github.com/flynn/flynn/pull/858))
- Read router listener port in tests correctly ([#862](https://github.com/flynn/flynn/pull/862))
- Report cluster client connection errors ([#863](https://github.com/flynn/flynn/pull/863))
- Fixed marshaling of JSON errors ([#874](https://github.com/flynn/flynn/pull/874))
- Fixed router retries of requests with bodies ([#875](https://github.com/flynn/flynn/pull/875))
- Fixed shutdown log caller attribution ([#878](https://github.com/flynn/flynn/pull/878))
- Don't attempt to write headers multiple times in streaming responses ([#881](https://github.com/flynn/flynn/pull/881))
- Fixed race in discoverd DNS tests ([#885](https://github.com/flynn/flynn/pull/885))
- Fixed CI log retrieval when adding hosts ([#890](https://github.com/flynn/flynn/pull/890))
- Fixed discoverd leader election ([#896](https://github.com/flynn/flynn/pull/896))
- Fixed race in discoverd health tests ([#902](https://github.com/flynn/flynn/pull/902))
- Avoided race in controller example generator ([#910](https://github.com/flynn/flynn/pull/910))
- Fixed race in flannel subnet file handling ([#912](https://github.com/flynn/flynn/pull/912))
- Fixed attach exit status regardless of stdin ([#914](https://github.com/flynn/flynn/pull/914))
- Fixed race in router registering with discoverd ([#925](https://github.com/flynn/flynn/pull/925))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)