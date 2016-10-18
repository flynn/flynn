---
title: Apps
layout: docs
toc_min_level: 2
---

# Apps

Applications can be deployed to Flynn using [Buildpacks](#buildpacks) or
[Docker](/docs/docker). This page provides information about the management and
configuration of apps on Flynn.

## Configuration

As suggested in [_The Twelve-Factor App_](http://12factor.net/config), Flynn
uses environment variables to configure applications.

The `flynn env` command is used to read and write environment variables.

```text
flynn env set SECRET=thisismysecret
```

Setting environment variables in Flynn creates a new release, which will restart
all of the app's processes with the new configuration.

### External Databases

Flynn apps can communicate with the [built-in databases](/docs/databases) as
well as databases hosted outside of Flynn. Pass the configuration for the
external database in as an environment variable with `flynn env`.

## Buildpacks

Flynn uses [buildpacks](https://devcenter.heroku.com/articles/buildpack-api) to
prepare and build apps deployed with `git push`. Flynn will automatically select
a standard buildpack for most supported languages.

The buildpack can be manually specified in cases where auto-detection is not
possible, or overridden when the standard buildpacks are not suitable.

The [multi buildpack](https://github.com/heroku/heroku-buildpack-multi) is
included in Flynn and can be used to specify a custom buildpack in addition to
allowing the use of multiple buildpacks during a single deploy.

To specify a custom buildpack, create and commit a `.buildpacks` file with one
or more URLs of buildpacks to use:

```text
https://github.com/kr/heroku-buildpack-inline
```

If you don't want to add a file to your app's repository to specify the
buildpack, you can also set the `BUILDPACK_URL` environment variable to specify
a custom buildpack:

```text
flynn env set BUILDPACK_URL=https://github.com/ryandotsmith/null-buildpack
```

## Deployment

Each time new code is pushed or the app configuration is changed, a new release
is created. Flynn deploys releases using a zero-downtime strategy, the new
release is started and the old release is only stopped if the new one comes up
correctly. If the new release does not come back up or something else goes
wrong, the deploy is automatically rolled back and the old release stays
running.

### Cancelling Deploys

Deploys via `git push` can be cancelled by killing the push process with
`Ctrl-C` or by signalling the process to terminate. The build will be cancelled
immediately and the code will not be deployed.

### Building specific Git branches

To deploy a different branch of the same repository, create a new app using the 
same git repository but with different remotes:

```
flynn create myapp-staging --remote staging
flynn -a staging env set FOO=bar
git push staging staging:master
```

## Processes

You can get a list of an app's individual processes using `flynn ps`. The ID
returned can then be passed to `flynn kill` to kill the process. Flynn will
automatically restart any killed processes that are not one-off run jobs.

```text
# Get a list of processes
$ flynn ps
ID                                          TYPE  STATE  CREATED        RELEASE                               COMMAND
host0-52aedfbf-e613-40f2-941a-d832d10fc400  web   up     6 seconds ago  cf39a906-38d1-4393-a6b1-8ad2befe8142  /runner/init start web

# Kill a process
$ flynn kill host-28a16c12-6136-4e06-93b1-2b014147de79
Job host-28a16c12-6136-4e06-93b1-2b014147de79 killed.
```

## Logs

Flynn automatically logs everything that app processes write to the standard
output and standard error streams. These logs can be retrieved with `flynn log`,
and can be followed in real time with `flynn log -f`.

### External Logs

Apps can also stream their logs to remote syslog services using system or client
libraries. Most programming languages have built-in support for remote logging,
for example Python's `SysLogHandler`.

## Routes

Flynn automatically configures a `https://$APPNAME.$CLUSTERDOMAIN` route that
points at instances of the `web` process type for each app. Apps must bind to
and accept HTTP requests at the port provided in the `PORT` environment variable
to receive traffic.

### Custom Domains

To add an additional HTTP route, use `flynn route add http`:

```text
flynn route add http www.example.com
```

DNS will also need to be configured for the domain, in this example
`www.example.com` should be set to a CNAME to `$APPNAME.$CLUSTERDOMAIN`.

### Additional Process Types

Flynn supports serving web traffic from multiple process types. These additional
process types must be defined in the `Procfile` and end in `-web`. For example, 
this Procfile:

```text
web: ./server
admin-web: ./admin-server
```

Routes for the additional process type can be configured by specifying the
`--service` flag:

```text
flynn route add http --service myapp-admin-web admin.example.com
```

### HTTPS

The router can automatically terminate HTTPS traffic, the certificate chain and
key are specified with the `--tls-cert` and `--tls-key` flags when creating or
updating the route. Enabling HTTPS for a route also enables HTTP/2
automatically.

```text
flynn route update http/2b3b2004-38f1-4e68-b856-7d8af3e4c6e1 --tls-cert cert.pem --tls-key cert.key
```

The certificate file should contain PEM-encoded certificate blocks for the
desired certificate followed by any intermediate certificates necessary to chain
to a trusted root.

### Service Discovery

Flynn automatically registers each web process type in service discovery for
internal requests that do not go through the router. The service discovery
entries are available via DNS. The pattern `$APPNAME-$PROCTYPE.discoverd` is
used for the DNS name, for example `myapp-web.discoverd` and
`myapp-admin-web.discoverd`. This feature can be used to communicate internally
between apps and processes.

## Limits

Memory and other resource limits can be retrieved and specified using the `flynn
limit` command. For example:

```text
flynn limit set web memory=2GB
```

### CPU Shares

CPU shares are relative, the more shares a process has, the higher priority it
is. When a host is under load, a job with 2000 milliCPUs will get twice the CPU
time as a job with the default of 1000.

```text
flynn limit set web cpu=1500
```

### Slugbuilder Limits

Some build processes require a lot of memory. If you encounter a slowness or
arbitrary failures during `git push` deploys, try increasing the memory limit of
the `slugbuilder` process:

```text
flynn limit set slugbuilder memory=4GB
```

You can also specify a default `slugbuilder` memory limit globally, set the
`SLUGBUILDER_DEFAULT_MEMORY_LIMIT` environment variable for the apps that handle
`git push` and Dashboard deploys:

```text
limit=SLUGBUILDER_DEFAULT_MEMORY_LIMIT=2GB
flynn -a gitreceive env set $limit
flynn -a taffy env set $limit
```
