---
title: How can I use disks for Flynn's ZFS pool?
layout: docs
toc_min_level: 2
---

# How can I use disks for Flynn's ZFS pool?

Prior to bootstrapping, you can create a ZFS pool with the Flynn cluster name, e.g. `flynn-default`, and Flynn will automatically use it.

    # Create the flynn-default ZFS pool
    $ sudo zpool create flynn-default /dev/sdb1

If you already have a Flynn cluster running, you can convert its ZFS pool to use disks by first attaching your disks, then detaching the sparsefile after it has been replicated to the newly-attached disks.

    # Attach /dev/sdb1 to the flynn-default ZFS pool
    $ sudo zpool attach flynn-default /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev /dev/sdb1

    # Check the replication status
    $ sudo zpool status
      pool: flynn-default
     state: ONLINE
      scan: resilvered 59.7M in 0h0m with 0 errors on Mon Nov  2 04:02:58 2015
    config:

    	NAME                                                          STATE     READ WRITE CKSUM
    	flynn-default                                                 ONLINE       0     0     0
    	  mirror-0                                                    ONLINE       0     0     0
    	    /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev  ONLINE       0     0     0
    	    sdb1                                                      ONLINE       0     0     0

    errors: No known data errors

    # Detach the sparsefile from the ZFS pool, and delete it
    $ sudo zpool detach flynn-default /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev
    $ sudo rm /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev
