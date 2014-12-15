---
title: "This Week in Flynn"
date: December 15, 2014
---

We made several improvements to the build workflow, adding proxy and root support to slugbuilder. A new command, `flynn-host run` will run jobs interactively, improving low-level debugging of a Flynn cluster.

Continuous Integration saw another round of improvements this week. Logs are now available from all instances and GitHub commits now link to the log stream.

We also deployed some fixes to underlying components, adding the latest security improvements to Docker image handling and upgrading to the final version of Go 1.4.

## Changes

### Enhancements

- Added proxy support to slugbuilder ([#542](https://github.com/flynn/flynn/issues/542))
- Slugbuilder optionally builds as root ([#552](https://github.com/flynn/flynn/issues/552))
- Node.js buildpack now has a recent version of NPM ([#583](https://github.com/flynn/flynn/issues/583))
- All instances on CI now retrieve logs ([#582](https://github.com/flynn/flynn/issues/582))
- If containers fail to start, they are now deleted ([#577](https://github.com/flynn/flynn/issues/577))
- Updated to Go 1.4 ([#586](https://github.com/flynn/flynn/issues/586))
- Updated Docker image handling code for security fixes ([#588](https://github.com/flynn/flynn/issues/588))
- Host state persistence is now tracked in BoltDB rather than JSON file ([#581](https://github.com/flynn/flynn/issues/581))
- Added `flynn-host run` command for low-level debugging ([#593](https://github.com/flynn/flynn/issues/593))
- GitHub commit status now includes a link to the CI log stream ([#570](https://github.com/flynn/flynn/issues/570))

### Bugfixes

- Fixed git error when running integration tests in the development VM ([#571](https://github.com/flynn/flynn/issues/571))
- Fixed containerinit error handling ([#593](https://github.com/flynn/flynn/issues/593))
- Fixed initialization bugs in `pkg/exec` ([#593](https://github.com/flynn/flynn/issues/593))
- Fixed terminal control forwarding in `flynn run` ([#593](https://github.com/flynn/flynn/issues/593))
- Fixed app links in the dashboard ([#595](https://github.com/flynn/flynn/issues/595))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
