---
title: How can I deploy a Docker image to Flynn?
layout: docs
toc_min_level: 2
---

# How can I deploy a Docker image to Flynn?

Flynn supports running Docker images from Docker registries. Note that currently any volumes declared in a Docker image are ephemeral and will be wiped when the instance reboots or is scaled down.

Start by creating a config file for your app, specifying the command to run, the job type, the init command, and any open ports.

Create a file called `config.json`, which you will pass to `flynn release add` via the `-f` flag.

```json
    {
      "processes": {
        "server": {
          "cmd": ["memcached", "-u", "nobody", "-l", "0.0.0.0", "-p", "11211", "-v"],
          "data": true,
          "ports": [{
            "port": 11211,
            "proto": "tcp",
            "service": {
              "name": "memcached",
              "create": true,
              "check": { "type": "tcp" }
            }
          }]
        }
      }
    }
```

Flynn requires the Docker image's registry URL. The easiest way to get this is to pull the image from the Docker registry and print its ID using the Docker CLI.

    $ docker pull memcached
    $ docker images --no-trunc|grep memcached
    memcached            latest               8937d6d027a4f52b877aacd9faa68b8a89652d858845e1ea2cf7bc6a8306db00   13 days ago         132.4 MB

Now, you can create the app via the Flynn CLI, create a release using the Docker registry url, and start the service by scaling it up.

    # Create the app.
    $ flynn create --remote "" memcached

    # Create a release using the Docker registry url.
    $ flynn -a memcached release add -f config.json "https://registry.hub.docker.com?name=memcached&id=8937d6d027a4f52b877aacd9faa68b8a89652d858845e1ea2cf7bc6a8306db00"

    # Start the service by scaling it up.
    $ flynn -a memcached scale server=1

Note that the last command may time out as Flynn must download the Docker image before it starts up, but it will eventually start. You can watch the process list for it to start with `flynn -a memcached ps`.

Your Flynn apps can now communicate with your new memcached service by connecting to `memcached.discoverd:11211`. Your app's hostname is based on the `service.name` key in the config file provided in the release.
