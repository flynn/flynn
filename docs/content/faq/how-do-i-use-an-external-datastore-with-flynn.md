---
title: How do I use redis/memcache/mysql/some other datastore with Flynn?
layout: docs
toc_min_level: 2
---

# How do I use redis/memcache/mysql/some other datastore with Flynn?

Flynn apps can communicate with services hosted outside of Flynn as usual, including MySQL, Redis, or Memcached. It is also possible to run a database or other datastore directly in Flynn by using a Docker image, though Flynn currently only supports ephemeral storage volumes with Docker images, so the data will not persist. This approach is suitable for Memcached, but it is recommended to use an external service, e.g. Amazon RDS or self-hosting, for datastores.
