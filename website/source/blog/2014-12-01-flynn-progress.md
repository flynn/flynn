---
title: "Flynn Progress: Week Ending Dec. 1, 2014"
date: December 1, 2014
---

Our test suite now covers every feature in the CLI! This will keep Flynn stable as we make future improvements. Integration testing has also improved and continues to be a major focus.

The user experience on [Flynn CI](https://ci.flynn.io/) continues to be refined with the addition of log streaming.

## Changes

### Enhancements

- Integration tests are now run concurrently, improving speed by 50%! The average duration is now 7m30s. ([#513](https://github.com/flynn/flynn/pull/513))
- Image unpacking is now done in a chroot, fixing several security issues. ([#519](https://github.com/flynn/flynn/pull/519))
- `flynn scale` displays scaling events. ([#520](https://github.com/flynn/flynn/pull/520))
- [Flynn CI](https://ci.flynn.io/) streams test logs while building. ([#525](https://github.com/flynn/flynn/pull/525))
- Added many integration tests to cover all of the CLI features. ([#501](https://github.com/flynn/flynn/pull/501))

### Bugfixes

- The default IP address is now parsed correctly. ([#508](https://github.com/flynn/flynn/pull/508))
- Private downloads from Docker Hub are authenticated correctly. ([#511](https://github.com/flynn/flynn/pull/511))
- Fixed a race in TCP route setup. ([#522](https://github.com/flynn/flynn/pull/522))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Email us](mailto:contact@flynn.io) whatever is on your mind!
