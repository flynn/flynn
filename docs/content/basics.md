---
title: Flynn Basics
layout: docs
toc_min_level: 2
---

# Flynn Basics

This guide assumes you already have a running Flynn cluster and have configured
the `flynn` command-line tool. If this is not the case, follow the [Installation
Guide](/docs/installation) first to get things set up.

It also assumes you are using the `demo.localflynn.com` default domain (which is
the case if you installed the Vagrant demo environment). If you are using your
own domain, substitute `demo.localflynn.com` with whatever you set
`CLUSTER_DOMAIN` to during the bootstrap process.

## Deploy

We will deploy a Go example application which starts a minimal HTTP server and
talks to a Postgres database.

Clone the Git repo:

```
$ git clone https://github.com/flynn-examples/go-flynn-example
```

Inside the cloned repo, create a Flynn application:

```
$ cd go-flynn-example
$ flynn create example
Created example
```

The above command should have added a `flynn` Git remote:

```
$ git remote -v
flynn   https://git.demo.localflynn.com/example.git (push)
flynn   https://git.demo.localflynn.com/example.git (fetch)
origin  https://github.com/flynn/nodejs-flynn-example.git (fetch)
origin  https://github.com/flynn/nodejs-flynn-example.git (push)
```

It should also have added a default route of `example.demo.localflynn.com` pointing
at the `example-web` service:

```
$ flynn route
ROUTE                             SERVICE      ID                                         STICKY  LEADER  PATH
http:example.demo.localflynn.com  example-web  http/2e37467e-08fc-47e5-853b-4f0574cb6871  false   false   /
```

The app depends on Postgres, so add a database:

```
$ flynn resource add postgres
Created resource 320f38ba-36bc-40ce-97e5-dad1b5c3bd20 and release c7b793ca-b7b1-4da0-bd1d-4ed95c1b52e8.
```

You can see the configuration for the database that the app will use:

```
$ flynn env
DATABASE_URL=postgres://84abab8e4000453fe2e1ce3f4f04392a:80f9191af9ae6c7890e4caae54990255@leader.postgres.discoverd:5432/7f02ee75fe57cb70fe1e5d9afc37935c
FLYNN_POSTGRES=postgres
PGDATABASE=7f02ee75fe57cb70fe1e5d9afc37935c
PGHOST=leader.postgres.discoverd
PGPASSWORD=80f9191af9ae6c7890e4caae54990255
PGUSER=84abab8e4000453fe2e1ce3f4f04392a
```

Push to the `flynn` Git remote to deploy the application:

```
$ git push flynn master
Counting objects: 728, done.
Delta compression using up to 8 threads.
Compressing objects: 100% (451/451), done.
Writing objects: 100% (728/728), 933.29 KiB | 0 bytes/s, done.
Total 728 (delta 215), reused 728 (delta 215)
-----> Building example...
-----> Go app detected
-----> Checking Godeps/Godeps.json file.
-----> Installing go1.6.3... done
-----> Running: go install -v -tags heroku .
-----> Discovering process types
       Procfile declares types -> web
-----> Compiled slug size is 3.6M
-----> Creating release...
=====> Scaling initial release to web=1
-----> Waiting for initial web job to start...
=====> Initial web job started
=====> Application deployed
To https://git.1.localflynn.com/example.git
 * [new branch]      master -> master
```

Now the application is deployed, you can make HTTP requests to it using the
default route for the application:

```
$ curl http://example.demo.localflynn.com
Hello from Flynn on port 8080 from container db0440f7-19b4-4369-b79e-7a48dba415c2
Hits = 1
```

## Scale

Applications declare their process types in a `Procfile` in the root directory.
The example application declares a single `web` process type which executes
`go-flynn-example`:

```
$ cat Procfile
web: go-flynn-example
```

New applications with a `web` process type are initially scaled to run one web
process, as can be seen with the `ps` command:

```
$ flynn ps
ID                                          TYPE  STATE  CREATED             RELEASE                               COMMAND
flynn-db0440f7-19b4-4369-b79e-7a48dba415c2  web   up     About a minute ago  ccd1aa34-77f7-4b7c-9772-e5d39a9f2d1e  /runner/init start web
```

Run more web processes using the `scale` command:

```
$ flynn scale web=3
scaling web: 1=>3

09:33:51.730 ==> web 4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9 pending
09:33:51.733 ==> web ccd3aff7-80b3-46b4-a95f-006bfceb80c6 pending
09:33:51.743 ==> web flynn-ccd3aff7-80b3-46b4-a95f-006bfceb80c6 starting
09:33:51.751 ==> web flynn-4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9 starting
09:33:52.129 ==> web flynn-4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9 up
09:33:52.171 ==> web flynn-ccd3aff7-80b3-46b4-a95f-006bfceb80c6 up

scale completed in 464.957638ms
```

`ps` should now show three running processes:

```
$ flynn ps
ID                                          TYPE  STATE  CREATED         RELEASE                               COMMAND
flynn-db0440f7-19b4-4369-b79e-7a48dba415c2  web   up     2 minutes ago   ccd1aa34-77f7-4b7c-9772-e5d39a9f2d1e  /runner/init start web
flynn-4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9  web   up     16 seconds ago  ccd1aa34-77f7-4b7c-9772-e5d39a9f2d1e  /runner/init start web
flynn-ccd3aff7-80b3-46b4-a95f-006bfceb80c6  web   up     16 seconds ago  ccd1aa34-77f7-4b7c-9772-e5d39a9f2d1e  /runner/init start web
```

Repeated HTTP requests should show that the requests are load balanced across
those processes and talk to the database:

```
$ curl http://example.demo.localflynn.com
Hello from Flynn on port 8080 from container db0440f7-19b4-4369-b79e-7a48dba415c2
Hits = 2

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 8080 from container 4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9
Hits = 3

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 8080 from container ccd3aff7-80b3-46b4-a95f-006bfceb80c6
Hits = 4

$ curl http://example.demo.localflynn.com
Hello from Flynn on port 8080 from container 4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9
Hits = 5
```

## Logs

You can view the logs (the stdout/stderr streams) of all processes running in
the app using the `log` command:

```
$ flynn log
2016-07-26T13:32:05.987763Z app[web.flynn-db0440f7-19b4-4369-b79e-7a48dba415c2]: hitcounter listening on port 8080
2016-07-26T13:33:52.370073Z app[web.flynn-4ef91e4b-d0c3-4e3f-931b-6db3b551dcd9]: hitcounter listening on port 8080
2016-07-26T13:33:52.402620Z app[web.flynn-ccd3aff7-80b3-46b4-a95f-006bfceb80c6]: hitcounter listening on port 8080
```

*See [here](/docs/cli#log) for more information on the `flynn log` command.*

## Release

New releases are created by committing changes to Git and pushing those changes
to Flynn.

Add the following line to the top of the `main()` function:

```text
fmt.Println("I've made a change!")
```

Commit that to Git and push the changes to Flynn:

```
$ git add main.go
$ git commit -m "Add log message"
$ git push flynn master
```

Once that push has succeeded, you should now see 3 new processes:


```
$ flynn ps
ID                                          TYPE  STATE  CREATED        RELEASE                               COMMAND
flynn-8f61a0f9-0582-474c-a996-1bec7d496f2a  web   up     6 seconds ago  677a8a2b-f67d-4e50-8712-1a1524a23b6f  /runner/init start web
flynn-f863b79a-d2b2-44d6-807b-1b508d758a8b  web   up     6 seconds ago  677a8a2b-f67d-4e50-8712-1a1524a23b6f  /runner/init start web
flynn-1f6b3c21-3b6f-4dc0-86b3-4bfb9481b71a  web   up     6 seconds ago  677a8a2b-f67d-4e50-8712-1a1524a23b6f  /runner/init start web
```

The logs of those processes should show the added log message:

```
$ flynn log -n 6
2016-07-26T13:37:01.634234Z app[web.flynn-1f6b3c21-3b6f-4dc0-86b3-4bfb9481b71a]: I've made a change!
2016-07-26T13:37:01.634509Z app[web.flynn-f863b79a-d2b2-44d6-807b-1b508d758a8b]: I've made a change!
2016-07-26T13:37:01.653521Z app[web.flynn-1f6b3c21-3b6f-4dc0-86b3-4bfb9481b71a]: hitcounter listening on port 8080
2016-07-26T13:37:01.654673Z app[web.flynn-f863b79a-d2b2-44d6-807b-1b508d758a8b]: hitcounter listening on port 8080
2016-07-26T13:37:01.666323Z app[web.flynn-8f61a0f9-0582-474c-a996-1bec7d496f2a]: I've made a change!
2016-07-26T13:37:01.677524Z app[web.flynn-8f61a0f9-0582-474c-a996-1bec7d496f2a]: hitcounter listening on port 8080
```

## Routes

On creation, the application's `web` process gets a default HTTP route which is a
subdomain of the default route domain (e.g. `example.demo.localflynn.com`). If
you want to use a different domain, you will need to add another route.

Let's say you have a domain `example.com` which is pointed at your Flynn cluster
(e.g. it is a `CNAME` for `example.demo.localflynn.com`).

Add a route for that domain:

```
$ flynn route add http example.com
http/74b05faf-c062-42f2-8ffe-678cfa3c061b
```

You should now have two routes for your application:

```
$ flynn route
ROUTE                             SERVICE      ID                                         STICKY  LEADER  PATH
http:example.com                  example-web  http/74b05faf-c062-42f2-8ffe-678cfa3c061b  false   false   /
http:example.demo.localflynn.com  example-web  http/2e37467e-08fc-47e5-853b-4f0574cb6871  false   false   /
```

HTTP requests to `example.com` should be routed to the web processes:

```
$ curl http://example.com
Hello from Flynn on port 8080 from container 8f61a0f9-0582-474c-a996-1bec7d496f2a
Hits = 6
```

You could now modify your application to respond differently based on the HTTP Host
header (which here could be either `example.demo.localflynn.com` or `example.com`).

## Multiple Processes

So far the example application has only had one process type (i.e. the `web` process),
but applications can have multiple process types which can be scaled individually.

Lets add a simple command that will print the time every second.

Make a new directory called `clock`, and add the following to `clock/main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	for t := range time.NewTicker(time.Second).C {
		fmt.Println(t)
	}
}
```

Create the `clock` process type by adding the following to `Procfile`:

```
clock: clock
```

Release these changes:

```
$ git add clock/ Procfile
$ git commit -m "Add clock service"
$ git push flynn master
```

Scale the `clock` service to one process and get its output:

```
$ flynn scale clock=1

$ flynn ps
ID                                          TYPE  STATE   CREATED         RELEASE                               COMMAND
flynn-5a2b2364-cfb6-411e-86f8-af9298994f09  clock  up     13 seconds ago  7f81b96d-3834-4eed-8b05-9ce66cc07b54  /runner/init start clock
flynn-c0b01f0f-4236-4d01-94eb-fe8c16f3dc0e  web    up     13 seconds ago  7f81b96d-3834-4eed-8b05-9ce66cc07b54  /runner/init start web
flynn-7453bc70-6b79-4776-9f32-c57506ba28f6  web    up     13 seconds ago  7f81b96d-3834-4eed-8b05-9ce66cc07b54  /runner/init start web
flynn-4fc38d7d-06d8-4285-a225-ae0cdeb58e03  web    up     13 seconds ago  7f81b96d-3834-4eed-8b05-9ce66cc07b54  /runner/init start web

$ flynn log -t clock
2016-07-26T13:47:11.217109Z app[clock.flynn-5a2b2364-cfb6-411e-86f8-af9298994f09]: 2016-07-26 13:47:11.216175147 +0000 UTC
2016-07-26T13:47:12.221625Z app[clock.flynn-5a2b2364-cfb6-411e-86f8-af9298994f09]: 2016-07-26 13:47:12.221232786 +0000 UTC
2016-07-26T13:47:13.217002Z app[clock.flynn-5a2b2364-cfb6-411e-86f8-af9298994f09]: 2016-07-26 13:47:13.21673476 +0000 UTC
2016-07-26T13:47:14.216574Z app[clock.flynn-5a2b2364-cfb6-411e-86f8-af9298994f09]: 2016-07-26 13:47:14.216298339 +0000 UTC
2016-07-26T13:47:15.216373Z app[clock.flynn-5a2b2364-cfb6-411e-86f8-af9298994f09]: 2016-07-26 13:47:15.21610766 +0000 UTC
```

## Run

An interactive one-off process may be spawned in a new container:

```
$ flynn run bash
```

*See [here](/docs/cli#run) for more information on the `flynn run` command.*
