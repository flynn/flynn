---
title: "Flynn Progress: Week Ending Nov. 24, 2014"
date: November 24, 2014
---
The Flynn web dashboard now ships by default as part of every cluster, whether you use the managed launcher or install on your own infrastructure. The web dashboard lets anyone deploy and scale GitHub apps on a running cluster without ever opening a terminal window. 

We've started building Flynn every night, so all new installations get the previous night's build. 

Test coverage has also improved and continues to be a major focus.
## Changes

### Enhancements

- Nightly builds are working. ([#469](https://github.com/flynn/flynn/pull/469))
- Scheduler tests are now integration tests. ([#186](https://github.com/flynn/flynn/pull/186))
- Pinkerton is now a package, the binary is no longer included in the Debian package.
  ([#477](https://github.com/flynn/flynn/pull/477))
- Request example generator for the controller. ([#473](https://github.com/flynn/flynn/pull/473))
- `CONTROLLER_DOMAIN` and `DEFAULT_ROUTE_DOMAIN` have been replaced with
  `CLUSTER_DOMAIN`. ([#266](https://github.com/flynn/flynn/pull/266))
- The dashboard is now included by default in bootstrap, making it available in all Flynn installations. Documentation around this is forthcoming. ([#266](https://github.com/flynn/flynn/pull/266))
- Buildpacks have been updated. The default OpenJDK is now version 8. ([#494](https://github.com/flynn/flynn/pull/494))
- Jobs may be run in the host network namespace, and discoverd/etcd now take
  advantage of this. ([#495](https://github.com/flynn/flynn/pull/495))
- Test durations are reported by [Flynn CI](https://ci.flynn.io). ([#443](https://github.com/flynn/flynn/pull/443))
- `flynn run` handles complex arguments better. ([#502](https://github.com/flynn/flynn/pull/502))
- `flynn inspect` includes the entrypoint and job command. ([#502](https://github.com/flynn/flynn/pull/502))

### Bugfixes

- Jobs that are stopped before they have fully started are now stopped
  correctly. ([#467](https://github.com/flynn/flynn/pull/467))
- IPv6 nameservers are filtered out of the resolv.conf used in containers.
  ([#474](https://github.com/flynn/flynn/pull/474))
- `flynn-host ps` no longer prints incorrect timestamps. ([#488](https://github.com/flynn/flynn/pull/488))
- Environment variables are exposed to buildpacks properly during builds. ([#483](https://github.com/flynn/flynn/pull/483))

## What's Next

We are focused on improving our test suite, documentation, and stability.

## Stay in Touch

* Star us on [GitHub](https://github.com/flynn/flynn)
* Join us on the [#flynn channel](http://webchat.freenode.net?channels=%23flynn) at Freenode
* Help solve an [easy issue](https://github.com/flynn/flynn/labels/easy)
* [Email us](mailto:contact@flynn.io) whatever is on your mind!
