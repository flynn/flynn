---
title: PostgreSQL
layout: docs
---

# PostgreSQL

The Flynn Postgres appliance provides PostgreSQL 9.4 in a highly-available
configuration with automatic provisioning. It automatically fails over to
a synchronous replica with no loss of data if the primary server goes down.

## Usage

### Adding a database to an app

Postgres comes ready to go as soon as you've installed Flynn. After you create
an app, you can provision a database for your app by running:

```text
flynn resource add postgres
```

This will provision a database on the Postgres cluster and configure your
application to connect to it.

### Connecting to the database

Provisioning the database will add a few environment variables to your app
release. `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGHOST` provide connection
details for the database and are used automatically by many Postgres clients.

Flynn will also create the `DATABASE_URL` environment variable which is utilized by some frameworks to configure database connections.

### Connecting to a console

To connect to a `psql` console for the database, run `flynn pg psql`. This does not
require the Postgres client to be installed locally or firewall/security
changes, as it runs in a container on the Flynn cluster.

### Dumping and restoring

The Flynn CLI provides commands for exporting and restoring database dumps.

`flynn pg dump` saves a complete copy of the database schema and data to a local file.

```text
$ flynn pg dump -f latest.dump
60.34 MB 8.77 MB/s
```

The file can be used to restore the database with `flynn pg restore`. It
may also be imported into a local Postgres database that is not managed by Flynn
with `pg_restore`:

```text
$ pg_restore --clean --no-acl --no-owner -d mydb latest.dump
```

`flynn pg restore` loads a database dump from a local file into a Flynn Postgres
database. Any existing tables and database objects will be dropped before they
are recreated.

```text
$ flynn pg restore -f latest.dump
62.29 MB / 62.29 MB [===================] 100.00 % 4.96 MB/s
WARNING: errors ignored on restore: 4
```

This will generate some warnings, but they are generally safe to ignore.

The restore command may also be used to restore a database dump from another non-Flynn
Postgres database, use `pg_dump` to create a dump file:

```text
$ pg_dump --format=custom --no-acl --no-owner mydb > mydb.dump
```

### Extensions

The Flynn Postgres appliance comes configured with many extensions available
including hstore, PostGIS, and PLV8. To enable an extension, use `CREATE
EXTENSION`:

```text
$ flynn pg psql
psql (9.4.1)
Type "help" for help.

bbabc090024fcdd118b04c50a0fb0d8c=> CREATE EXTENSION hstore;
CREATE EXTENSION
bbabc090024fcdd118b04c50a0fb0d8c=>
```

This is a complete list of the extensions that are available:

|        Name          | Version |                             Description                             |
|----------------------|---------|---------------------------------------------------------------------|
| btree\_gin           | 1.0     | support for indexing common datatypes in GIN                        |
| btree\_gist          | 1.0     | support for indexing common datatypes in GiST                       |
| chkpass              | 1.0     | data type for auto-encrypted passwords                              |
| citext               | 1.0     | data type for case-insensitive character strings                    |
| cube                 | 1.0     | data type for multidimensional cubes                                |
| dblink               | 1.1     | connect to other PostgreSQL databases from within a database        |
| dict\_int            | 1.0     | text search dictionary template for integers                        |
| earthdistance        | 1.0     | calculate great-circle distances on the surface of the Earth        |
| fuzzystrmatch        | 1.0     | determine similarities and distance between strings                 |
| hstore               | 1.3     | data type for storing sets of (key, value) pairs                    |
| intarray             | 1.0     | functions, operators, and index support for 1-D arrays of integers  |
| isn                  | 1.0     | data types for international product numbering standards            |
| ltree                | 1.0     | data type for hierarchical tree-like structures                     |
| pg\_prewarm          | 1.0     | prewarm relation data                                               |
| pg\_stat\_statements | 1.2     | track execution statistics of all SQL statements executed           |
| pg\_trgm             | 1.1     | text similarity measurement and index searching based on trigrams   |
| pgcrypto             | 1.1     | cryptographic functions                                             |
| pgrouting            | 2.0     | pgRouting Extension                                                 |
| pgrowlocks           | 1.1     | show row-level locking information                                  |
| pgstattuple          | 1.2     | show tuple-level statistics                                         |
| plpgsql              | 1.0     | PL/pgSQL procedural language                                        |
| plv8                 | 1.4.2   | PL/JavaScript (v8) trusted procedural language                      |
| postgis              | 2.1.5   | PostGIS geometry, geography, and raster spatial types and functions |
| postgis\_topology    | 2.1.5   | PostGIS topology spatial types and functions                        |
| postgres\_fdw        | 1.0     | foreign-data wrapper for remote PostgreSQL servers                  |
| tablefunc            | 1.0     | functions that manipulate whole tables, including crosstab          |
| unaccent             | 1.0     | text search dictionary that removes accents                         |
| uuid-ossp            | 1.0     | generate universally unique identifiers (UUIDs)                     |

Additionally, the following full text search dictionaries are installed by
default:

|      Name        |                        Description                        |
|------------------|-----------------------------------------------------------|
| danish\_stem     | snowball stemmer for danish language                      |
| dutch\_stem      | snowball stemmer for dutch language                       |
| english\_stem    | snowball stemmer for english language                     |
| finnish\_stem    | snowball stemmer for finnish language                     |
| french\_stem     | snowball stemmer for french language                      |
| german\_stem     | snowball stemmer for german language                      |
| hungarian\_stem  | snowball stemmer for hungarian language                   |
| italian\_stem    | snowball stemmer for italian language                     |
| norwegian\_stem  | snowball stemmer for norwegian language                   |
| portuguese\_stem | snowball stemmer for portuguese language                  |
| romanian\_stem   | snowball stemmer for romanian language                    |
| russian\_stem    | snowball stemmer for russian language                     |
| simple           | simple dictionary: just lower case and check for stopword |
| spanish\_stem    | snowball stemmer for spanish language                     |
| swedish\_stem    | snowball stemmer for swedish language                     |
| turkish\_stem    | snowball stemmer for turkish language                     |


## Design

The Flynn Postgres appliance was designed with a few goals in mind:

1. Acknowledged writes must not be lost and must be consistent.
1. Network partitions must be tolerated without corrupting data. There should be
   no potential for split-brain or other data-mangling failures.
1. When a failure occurs, the appliance should transition into an available
   configuration without operator intervention if it can do so safely.

The resulting system can be described as a 'CP' system under [CAP
theorem](https://en.wikipedia.org/wiki/CAP_theorem). The appliance is a cluster
of three or more Postgres instances where:

- One member of the cluster, the _primary_, serves consistent reads and writes.
- The primary has synchronous replication to a single member called the _sync_.
  Write transactions are not acknowledged to client until they have been added
  to the sync's transaction log.
- Replicating from the sync is a daisy chain of one or more _async_ instances,
  which replicate changes asynchronously from their upstream link in the chain.
- If possible, the system automatically reconfigures itself after failures to
  maximize uptime and never lose data.

In the face of an arbitrary failure or maintenance action, the cluster can
temporarily lose the ability to handle writes and consistent reads. Eventually
consistent reads are always available from the sync and async instances.

If the primary fails, the sync sees this and promotes itself to primary,
converting the async replicating from it to the new sync. Writes are not
accepted until the new sync has caught up. A variety of safety conditions are in
place so that a promotion will never cause writes to be lost or split brain to
occur.

The built-in Postgres streaming write ahead log replication features are wrapped
in a cluster state machine to achieve this. The cluster state is maintained by
the primary and stored in discoverd. The discoverd DNS and HTTP APIs expose the
current primary instance.

This design is heavily based on the prior work done by Joyent on the [Manatee
state machine](https://github.com/joyent/manatee-state-machine).

Flynn comes with a cluster configured with three instances by default. If an
instance fails, the scheduler will create a new instance and the cluster will be
reconfigured by the primary without operator intervention. When a user runs
`flynn resource add postgres`, a new user and database is created on the default
cluster.
