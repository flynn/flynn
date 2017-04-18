---
title: Production
layout: docs
toc_min_level: 2
---

# Production

We've designed Flynn to be very easy to get up and running with quickly.
However, there are a variety of things that should be considered as you start
using Flynn more.

## Cluster Requirements

At least three hosts are required to deploy Flynn in a highly available
configuration. We do not recommend single-node clusters for production use.
A three-host cluster will withstand the loss of one node with little to no
impact on availability.

All members of the initial cluster participate in Raft consensus voting, hosts
started after the initial bootstrap act as proxies to the consensus cluster. We
recommend starting with three or five hosts and adding more hosts when
necessary.

Each host should have a minimum of 2GB of memory, and inter-host network packets
should have a latency of less than 2ms. Deploying a single Flynn cluster across
higher latency WAN links is not recommended, as it can have a significant impact
on the stability of cluster consensus.

## Storage

Flynn uses ZFS to store data. By default, a ZFS pool is created in a sparse file
on top of the existing filesystem at `/var/lib/flynn/volumes`. We don't
recommend keeping this configuration in production, as it is not as reliable as
dedicating whole disks to the ZFS pool.

### Custom ZFS pool

The Flynn install script can be used to create the ZFS pool on the device of
your choice instead of a sparse file, using the `--zpool-create-device` flag:

```text
$ ./install-flynn --zpool-create-device /dev/sdb --zpool-create-options "-f"
```

If you already have a Flynn cluster running, you can move the existing ZFS pool
off of the sparse file by first attaching your disk as a mirror, then detaching
the sparse file after it has been replicated to the new disk:

```text
# Attach /dev/sdb1 (specify your disk instead of sdb1) to the flynn-default ZFS pool
$ sudo zpool attach flynn-default /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev /dev/sdb1

# Wait for the resilver to copy all data onto the newly added disk
$ sudo zpool status flynn-default
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

# Detach the sparse file from the ZFS pool and delete it
$ sudo zpool detach flynn-default /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev
$ sudo rm /var/lib/flynn/volumes/zfs/vdev/flynn-default-zpool.vdev
```

## Blobstore Backend

Flynn stores binary blobs like compiled applications, git repo archives,
buildpack caches, and Docker image layers using the blobstore component. The
blobstore supports multiple backends: Postgres, [Amazon
S3](https://aws.amazon.com/s3/), [Google Cloud
Storage](https://cloud.google.com/storage/), and [Microsoft Azure
Storage](https://azure.microsoft.com/en-us/services/storage/blobs/).

By default, the blobstore uses the built-in Postgres appliance to store these
blobs. This works well for light workloads and is the default configuration
because it allows Flynn to be deployed anywhere without any external service
dependencies.

We recommend using one of the external blobstore backends listed below for
production, as they are more reliable than the Postgres backend and have
predictable performance without consuming disk space on the Flynn hosts.

### Amazon S3

To migrate to the S3 backend, you first need to provision a new bucket and
create credentials for it. In AWS IAM, create a user with access credentials and
add a policy that looks like this:

```text
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:DeleteObject",
                "s3:GetObject",
                "s3:ListBucket",
                "s3:PutObject",
                "s3:ListMultipartUploadParts",
                "s3:AbortMultipartUpload",
                "s3:ListBucketMultipartUploads"
            ],
            "Resource": [
                "arn:aws:s3:::flynnblobstore",
                "arn:aws:s3:::flynnblobstore/*"
            ]
        }
    ]
}
```

Don't forget to save the credentials when creating the user and set the bucket
name in the `Resource` section before adding the policy.

After creating the S3 bucket and credentials, configure the blobstore to use it as
the backend with bucket, region, and access credentials:

```text
flynn -a blobstore env set BACKEND_S3MAIN="backend=s3 region=us-east-1 \
bucket=flynnblobstore access_key_id=$AWS_ACCESS_KEY_ID \
secret_access_key=$AWS_SECRET_ACCESS_KEY"

flynn -a blobstore env set DEFAULT_BACKEND=s3main
```

If the credentials are invalid, the first command will fail, and you can check the
logs with `flynn -a blobstore log`.

Finally, migrate the existing blobs from Postgres to S3 and remove them from
Postgres:

```text
flynn -a blobstore run /bin/flynn-blobstore migrate --delete
```

Or if you're on a version of Flynn older than v20160924.0:

```text
flynn -a blobstore run /bin/flynn-blobstore-migrate --delete
```

### Google Cloud Storage

To migrate to the Google Cloud Storage backend, you first need to create a new
[Service Account in
IAM](https://console.cloud.google.com/iam-admin/serviceaccounts). The account
does not need any roles, but make sure you create a private key and download the
JSON version. After creating the account, create a Cloud Storage bucket and add
the service account ID as a user with Owner access to the bucket permissions.

After creating the Cloud Storage bucket and credentials, configure the blobstore
to use it as the backend, with the bucket name and key file:

```text
flynn -a blobstore env set BACKEND_GCSMAIN="backend=gcs bucket=flynnblobstore" \
BACKEND_GCSMAIN_KEY="$(cat Project-7633a787c43f.json)"

flynn -a blobstore env set DEFAULT_BACKEND=gcsmain
```

If the credentials are invalid, the first command will fail, and you can check the
logs with `flynn -a blobstore log`.

Finally, migrate the existing blobs from Postgres to Cloud Storage and
remove them from Postgres:

```text
flynn -a blobstore run /bin/flynn-blobstore migrate --delete
```

### Microsoft Azure Storage

To migrate to the Azure Storage backend, you first need to create a storage
account and container. After setting those up, you should have an account name,
account key, and container name, which can be used to configure the backend:

```text
flynn -a blobstore env set BACKEND_AZUREMAIN="backend=azure account_key=xxx \
account_name=yyy container=flynnblobstore"

flynn -a blobstore env set DEFAULT_BACKEND=azuremain
```

If the credentials are invalid, the first command will fail, and you can check the
logs with `flynn -a blobstore log`.

Finally, migrate the existing blobs from Postgres to Azure Storage and
remove them from Postgres:

```text
flynn -a blobstore run /bin/flynn-blobstore migrate --delete
```


## DNS and Load Balancing

Flynn has a built-in router that handles all incoming HTTP, HTTPS and TCP
traffic. It is lightweight and runs on every Flynn host, this allows easy
deployment of Flynn without requiring another load balancer in front of Flynn.

We recommend running Flynn with low-TTL round-robin DNS A records pointing at
each host. For automatic failover, health checks should be configured that
automatically remove a host's A record if it is unhealthy.

Alternatively, a TCP load balancer for ports 80 and 443 can be configured in
front of the Flynn router, this may increase overhead and complexity but can
make sense in some environments.

## Firewalling

A firewall preventing external access must always be configured on or in front
of Flynn hosts, only these ports should be allowed:

* 80 (HTTP)
* 443 (HTTPS)
* 3000 to 3500 (user-defined TCP services, optional)

Internal cross-host cluster communication happens on a variety of UDP and TCP
ports and should not be restricted.

Outbound Internet access is required to deploy apps using many of the default
buildpacks.

## Automation

Installation and management of Flynn clusters can be automated using a variety
of existing tools. We recommend using tools that you are familiar with to create
a repeatable system for deploying Flynn clusters. This will allow you to quickly
start new clusters for testing, updates, and failure remediation.

When possible, we recommend deploying immutable infrastructure and deploying new
clusters for updates and major configuration changes.

## Backups

Flynn supports full-cluster backup/restore as well as export/import of
individual applications (including their databases).

### Cluster Backup

To take a full-cluster backup, run `flynn cluster backup --file backup.tar`.
A file name `backup.tar` will be created with a complete copy of all data and
configuration for the cluster. This includes full exports of all data in each
database managed by Flynn along with configuration necessary to start a new
cluster and restore the data.

### Cluster Restore

To restore from a full-cluster backup, follow [the manual installation
instructions](/docs/installation/manual) and modify the `flynn-host bootstrap`
command to include an extra flag: `--from-backup backup.tar`. The cluster size
does not need to be the same, but the `--min-hosts` flag and cluster discovery
flag should be specified. The `CLUSTER_DOMAIN` variable is ignored, the domain
of the previous cluster will be used.

### App Export

To export a single app, run `flynn -a APPNAME export --file app.tar`. A file
named `app.tar` will be created with the app configuration and image, along with
a copy of all data stored in associated databases. The app export can be
restored to the same cluster under a different name, or a different Flynn
cluster.

### App Import

To import an app, run `flynn import --file app.tar`. This will create a new app
on the cluster, import the configuration, create databases, and import the data
from the exported app. If you'd like to provide a new name for the app, the
`--name` flag may be specified. By default a new route is created based on the
app name and the cluster domain. To import the old routes in addition to the new
route, add the `--routes` flag.

## SSH Client Key

Some apps require a SSH private key to download dependencies or submodules while
deploying. Flynn supports configuring a single SSH client key for the platform
that will be used whenever SSH is used during builds triggered by `git push`.

To configure the keypair, set the `SSH_CLIENT_KEY` and `SSH_CLIENT_HOSTS`
environment variables on the built-in `gitreceive` app:

```text
flynn -a gitreceive env set SSH_CLIENT_HOSTS="$(ssh-keyscan -H github.com)"\
   SSH_CLIENT_KEY="$(cat ~/.ssh/id_rsa)"
```

## Monitoring

Flynn provides a status endpoint over HTTP that exposes the health of system
components at `http://status.$CLUSTER_DOMAIN` (for example,
`https://status.1.localflynn.com`). The status endpoint returns a status code
along with a more detailed JSON response. If any core components are unhealthy,
the HTTP status will be 500.

Requests to the status endpoint require a `key` parameter (example:
`https://status.1.localflynn.com/?key=$AUTH_KEY`) when they come from IP
addresses that are not reserved for private use.

The `$AUTH_KEY` may be retrieved with this command:

```text
flynn -a status env get AUTH_KEY
```

## Debugging

Flynn is a self-hosting system, this allows you to use the `flynn` and
`flynn-host` tools to introspect, configure, and debug the system. All
components with the exception of the `flynn-host` daemon appear as and can be
managed as apps in Flynn.

### Retrieving logs

Logs can be retrieved using several methods.

The `flynn -a $APP_NAME log` command will retrieve up to the last 10,000 lines
logged by all app processes and can also follow the stream as new log lines are
emitted.

The `flynn-host log $JOB_ID` command on a server will retrieve the logs for
a specific job and can also follow the stream for that job. A list of all jobs,
including those that are no longer running can be retrieved with the `flynn-host
ps -a` command.

Flynn stores logs from jobs in `/var/log/flynn` on each host. There is one log
file for jobs from each app that has been run on the host. All logs from jobs
tagged with the same app ID will go into a file named
`/var/log/flynn/$APP_ID.log`. When a log file reaches 100MB in size, it is
rotated and a new file is created. One previous rotated log is kept, for a total
of a maximum 200MB of logs per app per host.

Upstart manages the `flynn-host` daemon and stores the log at
`/var/log/upstart/flynn-host.log`.

The `flynn-host collect-debug-info` command will collect information about the
system it is run on along with recent logs from all apps and the `flynn-host`
daemon. By default it uploads these logs to [GitHub's
Gist](https://gist.github.com) service, but they can also be saved to a local
tarball with the `--tarball` flag.

### Internal Databases

The `controller`, `router`, and `blobstore` components store data in
a PostgreSQL cluster managed by Flynn. Their databases may be accessed by
running `flynn -a $APP_NAME pg psql`.

## Updating

There are two ways to update Flynn: in-place and backup/restore. The in-place
updater is new and we do not consider it to be stable. The most reliable
update method is backup/restore.

### Backup/Restore

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

### In-place update

The in-place updater is new and could cause cluster failure. We recommend taking
a full backup of the cluster first with `flynn cluster backup`.  There is almost
zero downtime during the cluster update, however database clusters may be
unavailable for a few seconds while they are updated.

To perform an in-place update of the entire cluster, run `flynn-host update`.

## Adding Hosts

Hosts may be added to an existing cluster by running `flynn-host init` with the
discovery token or list of host IPs that was used to start the cluster.

Care should be taken to ensure that the same version of Flynn is installed on
all hosts. The installed version of Flynn can be checked with `flynn-host
version`, and the version to install can be specified by setting the `--version`
CLI flag to the desired version when running the install script.

# Replacing Hosts

If a member of the cluster that is participating in the consensus set becomes
permanently unavailable, it must be replaced in order to restore fault
tolerance. If you have already added additional hosts to your cluster beyond the
members of the consensus set, you can promote one of these peers to the
consensus set. Otherwise you will first need to start a new host and join it to
the cluster as described in the Adding Hosts section. You can check the Raft
consensus status of your cluster hosts by running `flynn-host list`. If the host
is a member its status will be displayed as `peer`. If not, it will be listed as
`proxy`.

The first step is to demote the old peer from the consensus set. If the peer is
already unavailable, use the `--force` flag. If you are preemptively replacing
a peer that is currently working, the force flag should not be used.

To demote the peer, run `flynn-host demote --force $PEER_IP`.

Once the old peer is removed you can add the new peer to the consensus set with
`flynn-host promote $PEER_IP`. This may take a short time as the new peer
replicates data from the Raft leader before starting to service operations.

When the process is complete a `discoverd` deployment should be run to update
the `DISCOVERD_PEERS` environment variable. You can retrieve the current value
with `flynn -a discoverd env get DISCOVERD_PEERS`. Replace the address of the
old peer with the new one and update the value with `flynn -a discoverd env set
DISCOVERD_PEERS=$PEER_IPS`

At this point the host has been replaced successfully and the cluster will
regain full fault tolerance.

## Controller Keys

The Flynn CLI requires a controller authentication key to interact with
a cluster. The key is generated at cluster creation time, but it can be changed
and new keys can be added. Keys are stored in the environment variable
`AUTH_KEY`, a comma-separated list of authentication keys.

    # Generate a new random key
    NEW_KEY=$(openssl rand -hex 16)

    # Add the new key alongside the existing key
    flynn -a controller env set -t web AUTH_KEY=$NEW_KEY,$(flynn -a controller env get AUTH_KEY)

To rotate an authentication key:

    # Generate a new random key
    NEW_KEY=$(openssl rand -hex 16)

    # Add both the old and new keys to just the web process type
    flynn -a controller env set -t web AUTH_KEY=$NEW_KEY,$(flynn -a controller env get AUTH_KEY)

    # Update internal apps to use the new key
    flynn -a docker-receive env set AUTH_KEY=$NEW_KEY CONTROLLER_KEY=$NEW_KEY
    flynn -a gitreceive env set CONTROLLER_KEY=$NEW_KEY
    flynn -a taffy env set CONTROLLER_KEY=$NEW_KEY
    flynn -a dashboard env set CONTROLLER_KEY=$NEW_KEY
    flynn -a redis env set CONTROLLER_KEY=$NEW_KEY
    flynn -a mariadb env set CONTROLLER_KEY=$NEW_KEY
    flynn -a mongodb env set CONTROLLER_KEY=$NEW_KEY

    # Set the global key to be the new key
    flynn -a controller env set AUTH_KEY=$NEW_KEY

    # Unset the web process-specific key
    flynn -a controller env unset -t web AUTH_KEY
