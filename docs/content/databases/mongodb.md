---
title: MongoDB
layout: docs
---

# MongoDB

The Flynn MongoDB appliance provides MongoDB 3.2 in a highly-available
configuration with automatic provisioning. Replication is implemented using
MongoDB's replica set feature.

## Usage

### Adding a database to an app

MongoDB comes ready to go as soon as you've installed Flynn. After you create an
app, you can provision a database for your app by running:

```text
flynn resource add mongodb
```

This will provision a database on the MongoDB cluster and configure your
application to connect to it.

By default, MongoDB is not running in the Flynn cluster. The first time you
provision a database, MongoDB will be started and configured.

### Connecting to the database

Provisioning the database will add a few environment variables to your app
release. `MONGO_HOST`, `MONGO_USER`, `MONGO_PWD`, and `MONGO_DATABASE` provide
connection details for the database and are used automatically by many MongoDB
clients.

Flynn will also create the `DATABASE_URL` environment variable which is utilized
by some frameworks to configure database connections.

### Connecting to a console

To connect to a `mongo` console for the database, run `flynn mongodb mongo`.
This does not require the MongoDB client to be installed locally or
firewall/security changes, as it runs in a container on the Flynn cluster.

### Dumping and restoring

The Flynn CLI provides commands for exporting and restoring database dumps.

`flynn mongodb dump` saves a complete copy of the database to a local file.

```text
$ flynn mongodb dump -f latest.dump
60.34 MB 8.77 MB/s
```

The file can be used to restore the database with `flynn mongodb restore`. It may
also be imported into a local MongoDB database that is not managed by Flynn with
`mongorestore` and `tar` to extract the Flynn dump:

```text
$ mkdir dump # create a temporary directory for the dump
$ tar -x -C tmp latest.dump # extract the dump to the temporary directory
$ mongorestore tmp/* # you will need to use --host and auth flags as appropriate
```

`flynn mongodb restore` loads a database dump from a local file into a Flynn
MongoDB database. Any existing collections and objects will be dropped before
restoring data.

```text
$ flynn mongodb restore -f latest.dump
62.29 MB / 62.29 MB [===================] 100.00 % 4.96 MB/s
```

The restore command may also be used to restore a database dump from another
non-Flynn MongoDB database, use `mongodump` and `tar` to create a dump file:

```text
$ mkdir tmp # create a temporary directory
$ mongodump -o tmp # you will need to use --host and auth flags as appropriate
$ cd tmp
$ tar -cf ../mydb.dump .
```

### External access

An external route can be created that allows access to the database from
services that are not running on Flynn.

```text
flynn -a mongodb route add tcp --service mongodb --leader
```

This will provision a TCP port that always points at the primary instance.

For security reasons this port should be firewalled, and it should only be
accessed over the local network, VPN, or SSH tunnel.

## Safety

The MongoDB appliance uses a [replica
set](https://docs.mongodb.com/manual/replication/) for replication and is unsafe
by default, as all operations are replicated asynchronously.

To guarantee durability of writes, the [majority write
concern](https://docs.mongodb.com/manual/reference/write-concern/) should be
used. This will ensure writes are replicated before being acknowledged by the
client. If read consistency/isolation is required then the [majority read
concern](https://docs.mongodb.com/manual/reference/read-concern/) must also be
utilised. This setting ensures writes can only be read if they have been
committed to the other cluster members, it is otherwise possible to return
uncommitted data and potentially stale reads.
