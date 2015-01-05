---
title: "This Week in Flynn"
date: January 5, 2015
---

After several months of development, we've added [overlay networking](https://github.com/flynn/flynn/issues/423) to Flynn. Every container running in Flynn is now accessible through its own unique, cluster-routable IP address. This eliminates complicated networking configurations and opens the door to dramatic improvements in service discovery and cloud integration.

Also added this week, the Flynn dashboard now automatically detects and displays buildpack compatibility for apps deployed from GitHub. The interface will provide links to relevant documentation before you deploy your app, or warn you about missing buildpacks.

### Enhancements

- Added VXLAN overlay networking ([#668](https://github.com/flynn/flynn/pull/668))
- Updated dashboard to React v0.12.2 ([#671](https://github.com/flynn/flynn/pull/671))
- Added buildpack detection to dashboard ([#673](https://github.com/flynn/flynn/pull/673))
- Removed temporary CI debug logging ([#679](https://github.com/flynn/flynn/pull/679))
- Updated Ubuntu base image for VirtualBox ([#683](https://github.com/flynn/flynn/pull/683))
- Refactored streaming code to use a standard pattern ([#680](https://github.com/flynn/flynn/pull/680), [#684](https://github.com/flynn/flynn/pull/684))
- Replaced slow blobstore test with quick config check ([#685](https://github.com/flynn/flynn/pull/685))
- Clients will now try to dial each discovered service instance before giving up ([#687](https://github.com/flynn/flynn/pull/687))
- Updated Node.js buildpack to support specifying npm version ([#688](https://github.com/flynn/flynn/pull/688))
- Set default manifest locations for `flynn-host download` and `flynn-host bootstrap` ([#691](https://github.com/flynn/flynn/pull/691))
- Removed unused utility functions ([#692](https://github.com/flynn/flynn/pull/692))

### Bugfixes

- Fixed race in discoverd heartbeat ([#675](https://github.com/flynn/flynn/pull/675))
- Now hiding basic authentication details when downloading images ([#676](https://github.com/flynn/flynn/pull/676))
- Disabled memory overcommit on CI ([#677](https://github.com/flynn/flynn/pull/677))
- Fixed goroutine leak in logbuf ([#678](https://github.com/flynn/flynn/pull/678))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Let us know how you're using Flynn](mailto:contact@flynn.io)
