---
title: Redis
layout: docs
---

# Redis

The Flynn Redis appliance provides Redis 2.8 in a single process configuration.
The data for this process is ephemeral and is intended for caching and
development usage.


## Usage

### Adding a server to an app

Redis comes ready to go as soon as you've installed Flynn. After you create
an app, you can provision a server for your app by running:

```text
flynn resource add redis
```

This will provision a Redis server as a Flynn app and configure your application
to connect to it.

### Connecting to the database

Provisioning the database will add a few environment variables to your app
release. `REDIS_HOST`, `REDIS_PORT`, and `REDIS_PASSWORD` provide connection
details for the database.

Flynn will also create the `REDIS_URL` environment variable which is utilized
by some libraries to configure connections.

### Connecting to a console

To connect to a console for the database, run `flynn redis redis-cli`. This does
not require the Redis client to be installed locally or firewall/security
changes, as it runs in a container on the Flynn cluster.
