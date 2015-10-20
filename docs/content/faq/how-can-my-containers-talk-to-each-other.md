---
title: How can my containers talk to each other?
layout: docs
toc_min_level: 2
---

# How can my containers talk to each other?

Flynn provides DNS resolution for all apps that it runs. For internal and cross-container traffic, Flynn provides the local TLD `.discoverd`.

For web processes, Flynn uses the pattern `<app name>-web.discoverd`, e.g. `blog-web.discoverd`.

For datastores, for which Flynn provides leader election, the pattern is `<app name>.discoverd`, or to talk to the current leader, `leader.<app name>.discoverd`.
