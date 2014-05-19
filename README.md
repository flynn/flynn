# flynn-host

flynn-host is the [Flynn](https://flynn.io) host service. An instance of it runs
on every host in the Flynn cluster. It is responsible for running jobs (in
Docker containers) and reporting back to schedulers and the leader.

flynn-host is capable of bootstrapping itself inside of Docker containers with
its dependencies using the included manifest. It depends on
[discoverd](https://github.com/flynn/discoverd), which in turn depends on
[etcd](https://github.com/coreos/etcd). These three components are referred to
as "layer 0".

One instance of flynn-host acts as the "cluster leader" and serializes the
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

## Flynn 

[Flynn](https://flynn.io) is a modular, open source Platform as a Service (PaaS). 

If you're new to Flynn, start [here](https://github.com/flynn/flynn).

### Status

Flynn is in active development and **currently unsuitable for production** use. 

Users are encouraged to experiment with Flynn but should assume there are stability, security, and performance weaknesses throughout the project. This warning will be removed when Flynn is ready for production use.

Please report bugs as issues on the appropriate repository. If you have a general question or don't know which repo to use, report them [here](https://github.com/flynn/flynn/issues).

## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.
