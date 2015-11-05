---
title: Stability
layout: docs
toc_min_level: 2
---

# Stability

Every user has different requirements when it comes to stability.

The term "production ready" means very different things to different people.
We'd like to share what it means to us.

We believe software should only be described as "production ready" when it's
actually ready. Here's what we mean:

_The software is observed not to have a negative impact on SLAs in production.
Production-grade software also has stable lifecycle tooling including the
ability to update, monitor, debug, backup, and restore without having a negative
impact on uptime. We promise this is the only way we'll ever use the term._

Developers and organizations around the world are using Flynn in production
today. They're using Flynn in different ways based on their own needs and
assessments. The best way to know if Flynn is right for you is to try it.

We believe users should be able to choose the balance of stability and features
that is right for them. We are committed to transparency so you can decide which
features of Flynn to use at different times in different ways based on your own
specific needs.

We currently have two release channels: nightly and stable.

Nightly updates will include all the bleeding edge changes that have just been
merged into Flynn. These changes have all passed code review and our CI system,
but may not be fully tested in "the real world".

The stable channel is currently released weekly and changes have had more time
to stabilize. We can't guarantee that these releases will be free of bugs or
unexpected behavior, but our standards are and will continue to be high. It's
important to us that users feel they can trust us and the systems we build, and
that trust has to be earned. The release frequency will likely change over time
as we work with users and transition to a release model inspired by the release
train system used by major web browsers.

Flynn currently has some [security considerations](/docs/security) that you
should take into account when evaluating it.

Currently we do not recommend using the built-in Postgres appliance for
databases with high write volume or a large amount of data as it is not
yet optimized for this.
