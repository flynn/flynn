---
title: How do I get a dashboard login token?
layout: docs
toc_min_level: 2
---

# How do I get a dashboard login token?

Flynn's installer will provide a login token upon completion. If you lose or forget the token you can retrieve it using the CLI tool. Note that this requires having configured the cluster via `flynn cluster add`.

    $ flynn -a dashboard env get LOGIN_TOKEN
    0ff8d3b563d24c0d02fd25394eb86136
