# Router

[![](https://f.cloud.github.com/assets/13026/2060788/42916822-8c30-11e3-8c0d-ae743b905759.jpg)](https://commons.wikimedia.org/wiki/File:HebdrehwaehlerbatterieOrtsvermittlung_4954.jpg)

Router is the Flynn HTTP/TCP cluster router. It relies on [service
discovery](/discoverd) to keep track of what backends are up and acts as
a standard reverse proxy with random load balancing. HTTP domains and TCP ports
are provisioned via a HTTP API. Only two pieces of data are required: the domain
name and the service name. PostgreSQL is used as a pluggable persistence backend
so that all instances of router get the same configuration.

### Benefits over HAProxy/nginx

The primary benefits are that it uses service discovery natively and supports
dynamic configuration. Both HAProxy and nginx require a new process to be
spawned to change the majority of their configuration.
