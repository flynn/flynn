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

## Release Channels

We currently have two release channels: nightly and stable.

For details on current releases visit the [Flynn releases
site](https://releases.flynn.io).

Nightly updates will include all the bleeding edge changes that have just been
merged into Flynn. These changes have all passed code review and our CI system,
but may not be fully tested in "the real world".

Stable updates are released on the third Tuesday of each month with changes that
have had more time to stabilize. [Security updates](/docs/security) are provided
for the current and previous stable channel release.

Flynn currently has some [security considerations](/docs/security) that you
should take into account when evaluating it.

Currently we do not recommend using the built-in database appliances for
databases with high write volume or a large amount of data as they are not yet
optimized for demanding use cases.

## Release Mailing List

If you'd like to receive email about each month's stable release and security
updates, subscribe here:

<form action="https://flynn.us7.list-manage.com/subscribe/post?u=9600741fc187618e1baa39a58&id=8aadb709f3" method="post" target="_blank" novalidate class="mailing-list-form">
  <label>Email Address&nbsp;
    <input type="email" name="EMAIL" placeholder="you@example.com">
  </label>
  <button type="submit" name="subscribe">Subscribe</button>
</form>
