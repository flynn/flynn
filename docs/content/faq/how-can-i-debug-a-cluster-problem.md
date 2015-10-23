---
title: How can I debug a cluster problem?
layout: docs
toc_min_level: 2
---

# How can I debug a cluster problem?

Flynn's services are distributed across the hosts in your cluster, and most of them run in containers. The easiest way to collect the necessary logs and host information is to SSH to one of your Flynn cluster's hosts, and run `flynn-host collect-debug-info` as root.

    $ sudo flynn-host collect-debug-info

This will upload all of your logs to an anonymous gist, which you can go through yourself if you are experienced, or share with the Flynn developers on our IRC channel, [#flynn channel](https://webchat.freenode.net/?channels=flynn) on Freenode.
