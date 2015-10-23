---
title: How do I change or add a controller key?
layout: docs
toc_min_level: 2
---

# How do I change or add a controller key?

The Flynn CLI requires a controller authentication key to interact with a cluster. A key is generated at cluster creation time, but it can be changed and new keys can be added. Flynn uses the environment variable `AUTH_KEY`, which is a comma-separated list of authentication keys.

    # Generate a new random key
    NEW_KEY=$(openssl rand -hex 16)

    # Add the new key alongside the existing key
    flynn -a controller env set AUTH_KEY=$NEW_KEY,$(flynn -a controller env get AUTH_KEY)

To rotate an authentication key, add the new key to the controller web processes so both work simultaneously, then set the global controller key to be the new key, then you can remove the web process-specific key.

    # Generate a new random key
    NEW_KEY=$(openssl rand -hex 16)

    # Add both the old and new keys to just the web processes
    flynn -a controller env set -t web AUTH_KEY=$NEW_KEY,$(flynn -a controller env get AUTH_KEY)

    # Set the global key to be the new key
    flynn -a controller env set AUTH_KEY=$NEW_KEY

    # Unset the web process-specific key
    flynn -a controller env unset -t web AUTH_KEY
