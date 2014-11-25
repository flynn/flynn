# Flynn Host Service

`flynn-host` is the Flynn host service. An instance of it runs on every host in
the Flynn cluster. It is responsible for running jobs (in Linux containers) and
reporting back to schedulers and the leader.

flynn-host is capable of bootstrapping itself inside of containers with its
dependencies using the included manifest. It depends on [discoverd](/discoverd),
which in turn depends on [etcd](https://github.com/coreos/etcd). These three
components are referred to as "Layer 0".

One instance of host acts as the "cluster leader" and serializes the creation of
new jobs. The leader is maintains the entire cluster state (a list of hosts and
their running jobs) in memory. If the leader disappears, a new one is elected by
discoverd and the rest of the hosts connect to it and provide their current
state.
