---
title: Docker
layout: docs
toc_min_level: 2
---

# Docker

Flynn has a built-in `docker-receive` app which wraps a Docker registry and
imports pushed Docker images into a Flynn cluster.

## Configuration

Before pushing images to a Flynn cluster, both the local `flynn` and `docker`
CLIs need to be configured.

Configure the `flynn` CLI by running:

```
$ flynn docker set-push-url
```

Configure the `docker` CLI by running:

```
$ flynn docker login
```

_If you see "Error configuring docker", follow the instructions which appear
above the error then re-run `flynn docker login`._

## Push an image

Run the following to push a Docker image to `docker-receive` and deploy it:

```
$ flynn docker push IMAGE
```

where `IMAGE` is a reference to a Docker image which is available to the local
`docker` CLI (in other words, an image which appears in the output of `docker
images`).

## Using routes
In order to use flynn routes your container needs to listen to port 8080.

To make your container future proof it should listen to port defined in `$PORT` env.

It's easy to define `$PORT` env in your Dockerfile:

```Dockerfile
ENV PORT="8080"
```

This way flynn can override the port later when it will be dynamic instead of `8080`.

### Example

Here is an example of deploying the `elasticsearch:2.3.3` image from
[Docker Hub](https://hub.docker.com/_/elasticsearch/).

Pull the image from Docker Hub:

```
$ docker pull elasticsearch:2.3.3
```

Create an app:

```
$ flynn create --remote "" elasticsearch
Created elasticsearch
```

_NOTE: the `--remote ""` flag prevents Flynn trying to configure the local
git repository with a `flynn` remote, something which is useful only when
deploying with `git push` rather than `flynn docker push`._

Push the Docker image:

```
$ flynn -a elasticsearch docker push elasticsearch:2.3.3
flynn: getting image config with "docker inspect -f {{ json .Config }} elasticsearch:2.3.3"
flynn: tagging Docker image with "docker tag --force elasticsearch:2.3.3 docker.1.localflynn.com/elasticsearch:latest"
flynn: pushing Docker image with "docker push docker.1.localflynn.com/elasticsearch:latest"
The push refers to a repository [docker.1.localflynn.com/elasticsearch] (len: 1)
f44f27940253: Pushed
d7ed2fb18d43: Pushed
...
3c1b9f450611: Pushed
00f7d2b44350: Pushed
latest: digest: sha256:2447896a610a410aee1c351e33bfb489d2eb4875b4c17d83ef31add54b41b9a3 size: 18250
flynn: image pushed, waiting for artifact creation
flynn: deploying release using artifact URI http://flynn:dbd202007171356f4551160dede351ae@docker-receive.discoverd?name=elasticsearch&id=sha256:2447896a610a410aee1c351e33bfb489d2eb4875b4c17d83ef31add54b41b9a3
flynn: image deployed, scale it with 'flynn scale app=N'
```

You now have a release with an `app` process which runs using the pushed image
and has an `ENTRYPOINT`, `CMD` and `ENV` as taken from the Docker image's
config.

If this is the first deploy of the app, scale the `app` process to start it:

```
$ flynn -a elasticsearch scale app=1
scaling app: 0=>1

19:32:47.916 ==> app 0c363db0-80a4-41b8-b96f-1b490ff2b26d pending
19:32:47.926 ==> app host0-0c363db0-80a4-41b8-b96f-1b490ff2b26d starting
19:32:51.556 ==> app host0-0c363db0-80a4-41b8-b96f-1b490ff2b26d up

scale completed in 3.650229343s
```

The `app` process will be configured with a service name the same as the app's
name so your Flynn apps can communicate with the deployed service using
`APPNAME.discoverd:PORT` (e.g. `elasticsearch.discoverd:9200`):

```
$ flynn -a elasticsearch run curl http://elasticsearch.discoverd:9200
{
  "name" : "Emplate",
  "cluster_name" : "elasticsearch",
  "version" : {
    "number" : "2.3.3",
    "build_hash" : "218bdf10790eef486ff2c41a3df5cfa32dadcfde",
    "build_timestamp" : "2016-05-17T15:40:04Z",
    "build_snapshot" : false,
    "lucene_version" : "5.5.0"
  },
  "tagline" : "You Know, for Search"
}
```
