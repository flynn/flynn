---
title: How do I restart individual components?
layout: docs
toc_min_level: 2
---

# How do I restart individual components?

You can get a list of an app's individual processes, including that of Flynn's internal services, using `flynn ps`. The ID returned can then be passed to `flynn kill` to kill the process. Flynn will automatically restart any killed processes.

    # Get a list of processes
    $ flynn -a myapp ps
    ID                                         TYPE  RELEASE
    host-28a16c12-6136-4e06-93b1-2b014147de79  web   ace81d3d-93f5-4df3-b364-55f05cb908c3

    # Kill a process
    $ flynn -a myapp kill host-28a16c12-6136-4e06-93b1-2b014147de79
    Job host-28a16c12-6136-4e06-93b1-2b014147de79 killed.
