# Strowger

Strowger is the Flynn HTTP/TCP cluster router. It relies on [service
discovery](https://github.com/flynn/discoverd) to keep track of what backends
are up and acts as a standard reverse proxy with random load balancing. HTTP
domains and TCP ports are provisioned via RPC. Only two pieces of data are
required: the domain name and the service name. etcd is used for persistence so
that all instances of strowger get the same configuration.

### Benefits over HAProxy/nginx

The primary benefits are that it uses service discovery natively and supports
dynamic configuration. Both HAProxy and nginx require a new process to be
spawned to change the majority of their configuration.

Since this is very much an alpha prototype, a service discovery shim for HAProxy
would make more sense for production currently.
