---
title: Recently in Flynn
date: October 24, 2014
---

We're starting to report regularly on changes and new features in Flynn.

Flynn is a big project that spans a wide section of technology which makes it hard to tell what's new when you visit the website or GitHub repo. Posts like this will condense the most recent news so you can see what's happening without diving deep into the code.

If you have questions, suggestions, or other issues that aren't addressed here, we want to know about them! Please share through [IRC](irc://irc.freenode.net/flynn), [GitHub](https://github.com/flynn/flynn), or [email](mailto:contact@flynn.io).

## Summary

Our focus continues to be stability for core features (rather than adding new ones). Since the [beta release](/blog/flynn-beta) this summer we've been hard at work fixing bugs, writing tests, and making Flynn easier to use.

## Changes

### New Documentation

 - The [Installation guide](/docs/installation) has been expanded.
 - [Using Flynn](/docs/using-flynn) provides information on many basic tasks
   that can be accomplished with the Flynn command line interface.
 - [CLI documentation](/docs/cli) has been expanded and added to the website.
 - The [Development guide](/docs/development) describes how to make and test changes to the Flynn
   code.

### Notable Enhancements

 - Add subcommands to the `flynn-host` binary ([#181](https://github.com/flynn/flynn/pull/181))
 - Add utility for uploading debug info ([#233](https://github.com/flynn/flynn/pull/233))
 - Add `app delete` command to CLI ([#214](https://github.com/flynn/flynn/pull/214))
 - Allow overriding entrypoint in `flynn run` ([#193](https://github.com/flynn/flynn/pull/193))
 - Scale apps to web=1 on first push ([#208](https://github.com/flynn/flynn/pull/208))
 - Add support for wildcard routes ([#281](https://github.com/flynn/flynn/pull/281))

### Major Bugs Fixed

 - Blobstore transaction status idle ([#101](https://github.com/flynn/flynn/issues/101))
 - Various etcd usage issues ([#39](https://github.com/flynn/flynn/issues/39), [#48](https://github.com/flynn/flynn/issues/48), [#76](https://github.com/flynn/flynn/issues/76), [#187](https://github.com/flynn/flynn/issues/187))

### Summary

We've closed [over 280 issues and pull requests](https://github.com/flynn/flynn/issues?q=sort%3Aupdated-desc+closed%3A%3C2014-10-24+closed%3A%3E2014-08-13) and made [over 440 commits](https://github.com/flynn/flynn/compare/409d0051...bcda6fbb) since the beta release.

## What's Next

Flynn is moving towards production stability at a consistent pace.

In addition to stability improvements, expect Flynn to become easier to use, install, and understand.
