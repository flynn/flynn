---
title: MySQL
layout: docs
---

# MySQL

The Flynn MySQL appliance provides MariaDB 10.1 in a highly-available
configuration with automatic provisioning. It automatically fails over to
a synchronous replica with no loss of data if the primary server goes down.

## Usage

### Adding a database to an app

MariaDB comes ready to go as soon as you've installed Flynn. After you create an
app, you can provision a database for your app by running:

```text
flynn resource add mysql
```

This will provision a database on the MariaDB cluster and configure your
application to connect to it.

By default, MariaDB is not running in the Flynn cluster. The first time you
provision a database, MariaDB will be started and configured.

### Connecting to the database

Provisioning the database will add a few environment variables to your app
release. `MYSQL_HOST`, `MYSQL_USER`, `MYSQL_PWD`, and `MYSQL_DATABASE` provide
connection details for the database and are used automatically by many MySQL
clients.

Flynn will also create the `DATABASE_URL` environment variable which is utilized
by some frameworks to configure database connections.

### Connecting to a console

To connect to a `mysql` console for the database, run `flynn mysql console`.
This does not require the MySQL client to be installed locally or
firewall/security changes, as it runs in a container on the Flynn cluster.

### Dumping and restoring

The Flynn CLI provides commands for exporting and restoring database dumps.

`flynn mysql dump` saves a complete copy of the database schema and data to a local file.

```text
$ flynn mysql dump -f latest.dump
60.34 MB 8.77 MB/s
```

The file can be used to restore the database with `flynn mysql restore`. It may
also be imported into a local MySQL database that is not managed by Flynn with
`mysql`:

```text
$ mysql -D mydb < latest.dump
```

`flynn mysql restore` loads a database dump from a local file into a Flynn MySQL
database. Any existing tables and database objects will be dropped before they
are recreated.

```text
$ flynn mysql restore -f latest.dump
62.29 MB / 62.29 MB [===================] 100.00 % 4.96 MB/s
```

The restore command may also be used to restore a database dump from another non-Flynn
MySQL database, use `mysqldump` to create a dump file:

```text
$ mysqldump mydb > mydb.dump
```

### External access

An external route can be created that allows access to the database from
services that are not running on Flynn.

```text
flynn -a mariadb route add tcp --service mariadb --leader
```

This will provision a TCP port that always points at the primary instance.

For security reasons this port should be firewalled, and it should only be
accessed over the local network, VPN, or SSH tunnel.

## Safety

This appliance is designed to provide full consistency and partition tolerance
for all operations that are committed to the binlog. However, the semi-sync
replication configuration is not as well tested as our Postgres appliance, so
we do not have full confidence in the system yet.

There is currently no support for tuning, and data transfer during recovery is
not optimized, so we do not recommend using the appliance for applications that
have high throughput or many records.
