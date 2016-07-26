---
title: Databases
layout: docs
---

# Databases

Flynn includes built-in database appliances that handle configuring and managing
highly available databases automatically. These appliances are designed to
provide the maximum amount of safety available from the database system while
providing as much availability as possible without compromising safety.

In some cases it is not possible to meet the strict guarantees of a 'CP' system
under [CAP theorem](https://en.wikipedia.org/wiki/CAP_theorem) due to
limitations in the database software we are wrapping. This is noted specifically
in the Safety section of the documentation for the database in question.

Flynn's databases are currently designed with staging, testing, development, and
small-scale production workloads in mind. They are not currently suitable for
storing large amounts of data. We are in the process of making them usable for
all use cases, including high volume, large dataset workloads.

## State Machine Design

The Flynn database appliances are designed with a few goals in mind:

1. Acknowledged writes must not be lost and must be consistent.
1. Network partitions must be tolerated without corrupting data. There should be
   no potential for split-brain or other data-mangling failures.
1. When a failure occurs, the appliance should transition into an available
   configuration without operator intervention if it can do so safely.

 The appliance is a cluster of three or more database instances where:

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

The cluster state is maintained by the primary and stored in discoverd. The
discoverd DNS and HTTP APIs expose the current primary instance.

This design is heavily based on the prior work done by Joyent on the [Manatee
state machine](https://github.com/joyent/manatee-state-machine).

Flynn comes with a cluster configured with three instances by default. If an
instance fails, the scheduler will create a new instance and the cluster will be
reconfigured by the primary without operator intervention.
