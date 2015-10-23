---
title: How do I check logs in Flynn?
layout: docs
toc_min_level: 2
---

# How do I check logs in Flynn?

Flynn collects logs from all of an app's processes. The dashboard allows you to see each process's logs individually with live updating, while the CLI offers more control over what you are looking at.

    # View logs for all processes
    $ flynn log

    # Follow live logs
    $ flynn log -f

    # Just web processes
    $ flynn log -t web
