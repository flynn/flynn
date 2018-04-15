---
title: Docker
layout: docs
toc_min_level: 2
---

# Docker

Flynn has a built-in `tarreceive` app which imports pushed Docker images into a
Flynn cluster.

## Push an image

Run the following to push a Docker image to `tarreceive` and deploy it:

```
$ flynn -a APPNAME docker push IMAGE
```

where `APPNAME` is the name of an existing Flynn app and `IMAGE` is a reference
to a Docker image which is available to the local `docker` CLI (in other words,
an image which appears in the output of `docker images`).

## Routing

Flynn automatically registers the HTTP route `http://APPNAME.$CLUSTER_DOMAIN`
for the app. In order to receive HTTP traffic for this route, the app needs to
listen on the port which is set in the `PORT` environment variable.

## Example

Here is an example of deploying the [Flynn Node.js example app](https://github.com/flynn-examples/nodejs-flynn-example)
using `flynn docker push`.

Clone the git repository:

```
$ git clone https://github.com/flynn-examples/nodejs-flynn-example.git
$ cd nodejs-flynn-example
```

Build the Docker image:

```
$ docker build -t nodejs-flynn-example .
```

Create an app:

```
$ flynn create --remote "" nodejs
Created nodejs
```

_NOTE: the `--remote ""` flag prevents Flynn trying to configure the local
git repository with a `flynn` remote, something which is useful only when
deploying with `git push` rather than `flynn docker push`._

Push the Docker image:

```
$ flynn -a nodejs docker push nodejs-flynn-example
deploying Docker image: nodejs-flynn-example
exporting image with 'docker save nodejs-flynn-example'
650.67 MB 107.82 MB/s 6s
uploading layer 8fad67424c4e7098f255513e160caa00852bcff347bc9f920a82ddf3f60229de
123.29 MB 22.98 MB/s 5s
uploading layer 86985c679800f423275a0ea3ad540e9b7f522dcdcd65ec2b20f407996162f2e0
43.35 MB 24.49 MB/s 1s
uploading layer 6e5e20cbf4a7246b94f7acf2a2ceb2c521e95daca334dd1e8ba388fa73443dfe
121.03 MB 11.38 MB/s 10s
uploading layer ff57bdb79ac820da132ad1fdc1e2d250de5985b264dbdf60aa4ce83a05c4da75
313.16 MB 7.18 MB/s 43s
uploading layer 0e0b4ee1c6dc1f57a46071fc075ecf66b3164e637096197f10b89b6086942c7a
344.50 KB 632.22 KB/s 0s
uploading layer 33aed7748ee38dd620489fce6366fab070591e3b5ec276b5f505ed957772d7bc
132.00 KB 3.73 MB/s 0s
uploading layer 3b227e2efc63768cd4c62d5e7e6678b98064bf5bdbc40fc845913e970ccc5610
44.96 MB 13.26 MB/s 3s
uploading layer 984a57f9e68ea548ace7b655095d109a94850c918d10707ac4c922c9d9778136
4.25 MB 36.01 MB/s 0s
uploading layer 38a320798e65a1b2faed502dcac756feddcc85b3e83d72d7a57998fc992a2564
4.00 KB 159.07 KB/s 0s
uploading layer 148577a474a5c57422278e836bf204288fae7bf9f3ab5d6643666c30abb9bd87
5.50 KB 150.91 KB/s 0s
uploading layer 7779527ad11c4b4f1edb62e7f1391ee6cd82df898b8e96a16822ae79124669e2
108.00 KB 2.22 MB/s 0s
Docker image deployed, scale it with 'flynn scale app=N'

```

You now have a release with an `app` process which runs using the pushed image
and has an `ENTRYPOINT`, `CMD` and `ENV` as taken from the Docker image's
config.

If this is the first deploy of the app, scale the `app` process to start it:

```
$ flynn -a nodejs scale app=1
scaling app: 0=>1

16:24:31.397 ==> app 4a7319af-af2c-4fe1-9a9a-2dd4d5bd3765 pending
16:24:31.398 ==> app host0-4a7319af-af2c-4fe1-9a9a-2dd4d5bd3765 starting
16:24:31.527 ==> app host0-4a7319af-af2c-4fe1-9a9a-2dd4d5bd3765 up

scale completed in 140.63784ms
```

The `app` process will be configured with a service name like `APPNAME-web` so
your Flynn apps can communicate with the deployed service internally using
`APPNAME-web.discoverd:PORT` (e.g. `nodejs-web.discoverd:8080`):

```
$ flynn -a nodejs run curl http://nodejs-web.discoverd:8080
Hello from Flynn on port 8080 from container 4a7319af-af2c-4fe1-9a9a-2dd4d5bd3765
```

The app can be reached externally via the automatically registered route
`http://APPNAME.$CLUSTER_DOMAIN`:

```
$ curl http://nodejs.1.localflynn.com
Hello from Flynn on port 8080 from container 4a7319af-af2c-4fe1-9a9a-2dd4d5bd3765
```
