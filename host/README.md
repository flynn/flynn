# Host

Host is the [Flynn](https://flynn.io) host service. An instance of it runs
on every host in the Flynn cluster. It is responsible for running jobs (in
Linux containers) and reporting back to schedulers and the leader.

Host is capable of bootstrapping itself inside of Docker containers with
its dependencies using the included manifest. It depends on
[discoverd](/discoverd), which in turn depends on
[etcd](https://github.com/coreos/etcd). These three components are referred to
as "layer 0".

One instance of host acts as the "cluster leader" and serializes the
creation of new jobs. The leader is maintains the entire cluster state (a list
of hosts and their running jobs) in memory. If the leader disappears, a new one
is elected by discoverd and the rest of the hosts connect to it and provide
their current state.

## Usage

The host service expects to be run inside of a Docker container with access to
the Docker socket. It also needs to know the external IP address that can be
used to reach it (and every container it runs inside of Docker).

```text
IP=$(ifconfig eth0 | grep 'inet addr:' | cut -d: -f2 | awk '{print $1}')
docker run -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113 flynn/host -external $IP
```

Subsequent hosts should pass a list of comma separated peers to etcd containing
at least one running host:

```text
docker run -v=/var/run/docker.sock:/var/run/docker.sock -e=ETCD_PEERS=10.1.2.10:7001 -p=1113:1113 flynn/host -external $IP
```

The etcd [discovery
service](https://coreos.com/docs/cluster-management/setup/etcd-cluster-discovery/)
may also be used by setting the `ETCD_DISCOVERY` environment variable.

The `-force` option, if provided, will terminate all existing containers that
were booted by a previous instance of the host service.

## TODO

- Recover from crashes/restarts
- Increase test coverage
- Documentation
