---
title: How do I use custom buildpacks?
layout: docs
toc_min_level: 2
---

# How do I use custom buildpacks?

Flynn uses Heroku buildpacks to prepare and build apps. Flynn will automatically select a standard buildpack for most supported languages. It can be manually specified in cases where auto-detection is not possible, or overridden when the standard buildpacks are not suitable. To set a custom buildpack, simply set the `BUILDPACK_URL` environment variable in the dashboard before launching or deploying a new revision. Flynn will automatically download that buildpack and use it to build your app.

![buildpack url](/images/docs/buildpack-url.png)
