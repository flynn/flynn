---
title: Using Flynn
layout: docs
---

# Using Flynn

This guide assumes you already have a running Flynn cluster and have configured
the `flynn` command-line tool. If this is not the case, follow the [Installation
Guide](/docs/installation) first to get things set up.

It also assumes you are using the `demo.localflynn.com` default domain (which is
the case if you installed the demo environment). If you are using your own
domain, substitute `demo.localflynn.com` with whatever you set `CLUSTER_DOMAIN`
to during the bootstrap process.

## Add SSH key

Before deploying to Flynn, you need to add your public SSH key:

```
$ flynn key add
Key dd:31:2c:07:33:fb:93:32:2b:cc:fa:87:a4:0f:00:34 added.
```

*See [here](/docs/cli#key) for more information on the `flynn key` command.*

## Deploy

We will deploy a Node.js example application which starts a minimal HTTP server.

Clone the Git repo:

```
$ git clone https://github.com/flynn/nodejs-flynn-example.git
```

Inside the cloned repo, create a Flynn application:

```
$ cd nodejs-flynn-example
$ flynn create example
Created example
```

The above command should have added a `flynn` Git remote:

```
$ git remote -v
flynn   ssh://git@demo.localflynn.com:2222/example.git (push)
flynn   ssh://git@demo.localflynn.com:2222/example.git (fetch)
origin  https://github.com/flynn/nodejs-flynn-example.git (fetch)
origin  https://github.com/flynn/nodejs-flynn-example.git (push)
```

It should also have added a default route of `example.demo.localflynn.com` pointing
at the `example-web` service:

```
$ flynn route
ROUTE                             SERVICE      ID
http:example.demo.localflynn.com  example-web  http/1ba949d1654e711d03b5f1e471426512
```

Push to the `flynn` Git remote to deploy the application:

```
$ git push flynn master
...
-----> Building example...
-----> Node.js app detected
...
-----> Creating release...
=====> Application deployed
=====> Added default web=1 formation
To ssh://git@demo.localflynn.com:2222/example.git
 * [new branch]      master -> master
```

Now the application is deployed, you can make HTTP requests to it using the
default route for the application:

```
$ curl http://example.demo.localflynn.com
Hello from Flynn on port 55006 from container d55c7a2d5ef542c186e0feac5b94a0b0
```

## Scale

Applications declare their process types in a `Procfile` in the root directory. The
example application declares a single `web` process type which executes `web.js`:

```
$ cat Procfile
web: node web.js
```

New applications with a `web` process type are initially scaled to run one web
process, as can be seen with the `ps` command:

```
$ flynn ps
ID                                      TYPE
flynn-d55c7a2d5ef542c186e0feac5b94a0b0  web
```

Run more web processes using the `scale` command:

```
$ flynn scale web=3
```

`ps` should now show three running processes:

```
$ flynn ps
ID                                      TYPE
flynn-7c540dffaa7e434db3849280ed5ba020  web
flynn-3e8572dd4e5f4136a6a2243eadca5e02  web
flynn-d55c7a2d5ef542c186e0feac5b94a0b0  web
```

Repeated HTTP requests should show that the requests are load balanced across
those processes:

```
$ curl http://example.demo.localflynn.com
Hello from Flynn on port 55006 from container d55c7a2d5ef542c186e0feac5b94a0b0

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 55007 from container 3e8572dd4e5f4136a6a2243eadca5e02

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 55008 from container 7c540dffaa7e434db3849280ed5ba020

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 55007 from container 3e8572dd4e5f4136a6a2243eadca5e02
```

## Logs

You can view the logs (i.e. stdout / stderr) of a job using the `log` command:

```
$ flynn log flynn-d55c7a2d5ef542c186e0feac5b94a0b0
Listening on 55006
```

*See [here](/docs/cli#log) for more information on the `flynn log` command.*

## Release

New releases are created by committing changes to Git and pushing those changes
to Flynn.

Add the following line to the top of `web.js`:

```js
console.log("I've made a change!")
```

Commit that to Git and push the changes to Flynn:

```
$ git add web.js
$ git commit -m "Add log message"
$ git push flynn master
```

Once that push has succeeded, you should now see 3 new processes:


```
$ flynn ps
ID                                      TYPE
flynn-cf834b6db8bb4514a34372c8b0020b1e  web
flynn-16f2725f165343fca22a65eebab4e230  web
flynn-d7893da39a8847f395ce47f024479145  web
```

The logs of those processes should show the added log message:

```
$ flynn log flynn-cf834b6db8bb4514a34372c8b0020b1e
I've made a change!
Listening on 55007
```

## Routes

On creation, applications get a default route which is a subdomain of the default
route domain (e.g. `example.demo.localflynn.com`). If you want to use a different
domain, you will need to add another route.

Let's say you have a domain `example.com` which is pointed at your Flynn cluster
(e.g. it is a `CNAME` for `example.demo.localflynn.com`).

Add a route for that domain:

```
$ flynn route add http example.com
http/5ababd603b22780302dd8d83498e5172
```

You should now have two routes for your application:

```
$ flynn route
ROUTE                             SERVICE      ID
http:example.com                  example-web  http/5ababd603b22780302dd8d83498e5172
http:example.demo.localflynn.com  example-web  http/1ba949d1654e711d03b5f1e471426512
```

HTTP requests to `example.com` should be routed to the web processes:

```
$ curl http://example.com
Hello from Flynn on port 55007 from container cf834b6db8bb4514a34372c8b0020b1e
```

You could now modify your application to respond differently based on the HTTP Host
header (which here could be either `example.demo.localflynn.com` or `example.com`).

## Multiple Processes

So far the example application has only had one process type (i.e. the `web` process),
but applications can have multiple process types which can be scaled individuallly.

Lets add a simple `clock` service which will print the time every second.

Add the following to `clock.js`:

```js
setInterval(function() {
  console.log(new Date().toTimeString());
}, 1000);
```

Create the `clock` process type by adding the following to `Procfile`:

```
clock: node clock.js
```

Release these changes:

```
$ git add clock.js Procfile
$ git commit -m "Add clock service"
$ git push flynn master
```

Scale the `clock` service to one process and get its output:

```
$ flynn scale clock=1

$ flynn ps
ID                                      TYPE
flynn-aff3ae71d0c149b185ec64ea2885075f  clock
flynn-cf834b6db8bb4514a34372c8b0020b1e  web
flynn-16f2725f165343fca22a65eebab4e230  web
flynn-d7893da39a8847f395ce47f024479145  web

$ flynn log flynn-aff3ae71d0c149b185ec64ea2885075f
18:40:42 GMT+0000 (UTC)
18:40:43 GMT+0000 (UTC)
18:40:44 GMT+0000 (UTC)
18:40:45 GMT+0000 (UTC)
18:40:46 GMT+0000 (UTC)
...
```

## Run

An interactive one-off process may be spawned in a container:

```
$ flynn run bash
```

*See [here](/docs/cli#run) for more information on the `flynn run` command.*
