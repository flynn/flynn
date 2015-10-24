---
title: How can I pass configuration to my app?
layout: docs
toc_min_level: 2
---

# How can I pass configuration to my app?

The recommended way to pass configuration to your app is via environment variables, as is commonly done with Heroku or Docker.

Flynn deploys apps directly from a git repository without a configuration management system such as Chef or Puppet to handle configuration files, and storing configuration -- in particular secrets -- in git is generally discouraged.

This has the added benefits of supporting multiple arbitrary environments without a dedicated configuration file for each, and making configuration changes without going through the cycle of code change, commit, code review, and deploy.

To set a configuration variable via the CLI, use `flynn env set NAME=value`, e.g.:

    # Set the environment variable SECRET in myapp
    flynn -a myapp env set SECRET=thisismysecret

Setting environment variables in Flynn creates a new release, which will restart all of the app's processes.
