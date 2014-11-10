---
title: "Flynn Progress: Week Ending Nov. 9, 2014"
date: November 10, 2014
---

Last week was all about our primary goal: making Flynn stable.

As always, we are continuing to close issues, but we want to make it easier for you to follow along with our progress. So we are working towards a release mechanism for nightly builds. We plan to ship that next week!

## Changes

### Notable Enhancements

* Our shell scripts have been [reformatted to match Google's style guide.](https://github.com/flynn/flynn/pull/368)
* Slugrunner can now [follow redirects when downloading slugs](https://github.com/flynn/flynn/pull/403). This can be very useful when using slugrunner standalone with private slugs.
* We've [completely automated our release tooling](https://github.com/flynn/flynn/issues/252) so builds can be pushed more frequently. We'll be adding nightly builds and more visibility around that soon.

### Major Bugs Fixed

* Job errors are propagated via the attach protocol and container setup errors are properly handled. ([#406](https://github.com/flynn/flynn/pull/406))
* Intermittent container EBADF failures. ([#402](https://github.com/flynn/flynn/issues/402))
* PHP HHVM support. ([#365](https://github.com/flynn/flynn/issues/365))

## What's Next

Flynn is moving towards production stability at a consistent pace. We expect to have nightly builds available next week. We continue to work hard on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Email us](mailto:contact@flynn.io) whatever is on your mind!
