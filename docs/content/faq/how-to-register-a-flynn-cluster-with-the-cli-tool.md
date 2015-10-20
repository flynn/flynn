---
title: How to register a Flynn cluster with the CLI tool
layout: docs
toc_min_level: 2
---

# How to register a Flynn cluster with the CLI tool

When a cluster is bootstrapped, a self-signed certificate is created. Flynn will provide a fingerprint of this certificate, which must be passed to the Flynn CLI tool to add a cluster. A controller key is also generated at cluster creation time, though it can be changed or additional ones added.

    flynn cluster add -g <git host> -p <tls pin> <cluster name> <controller URL> <controller key>

* `-g <git host>` specifies the URL of the controller.
* `-p <pin>` certificate pin
* `cluster name` can be any name
* `controller url` will be controller.[whatever domain you picked]
* `controller key` is a controller authentication key.

Example:

    flynn cluster add -g dev.localflynn.com:2222 -p KGCENkp53YF5OvOKkZIry71+czFRkSw2ZdMszZ/0ljs= default https://controller.dev.localflynn.com e09dc5301d72be755a3d666f617c4600
    Cluster "default" added and set as default.
