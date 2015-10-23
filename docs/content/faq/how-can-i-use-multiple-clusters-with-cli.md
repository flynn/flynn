---
title: How can I use multiple Flynn clusters with the CLI tool?
layout: docs
toc_min_level: 2
---

# How can I use multiple Flynn clusters with the CLI tool?

The Flynn CLI supports multiple clusters out of the box. Use the `-c` flag to specify a cluster by name.

    # List apps on a Flynn cluster named production
    $ flynn -c production apps
