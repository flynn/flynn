---
title: How do I update Flynn?
layout: docs
toc_min_level: 2
---

# How do I update Flynn?

There are two ways to update Flynn: in-place and backup/restore. The in-place
updater is new and we do not consider it to be fully stable. The most reliable
update method is backup/restore.

## Backup/Restore

The backup/restore update method involves taking a full backup of the cluster
and restoring it to a new cluster with the new version of Flynn.

1. Take a backup of the cluster with `flynn cluster backup --file backup.tar`.
2. Install the new version of Flynn on a new cluster by following [the manual
   installation instructions](/docs/installation/manual) up to but not including
   the bootstrap step.
3. Run the `flynn-host bootstrap` command from the installation guide with an
   added flag pointing at the cluster backup file: `flynn-host bootstrap
   --from-backup backup.tar`
4. Update the DNS records that point at the old cluster to point at the new
   cluster.

## In-place update

The in-place updater is new and may cause unpredictable issues, we recommend
taking a full backup of the cluster before starting with `flynn cluster backup`.
There is almost zero downtime during the cluster update, however the Postgres
cluster may be unavailable for a few seconds while it is being updated.

To perform an in-place update of the entire cluster, run `flynn-host update`.
