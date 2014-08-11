# pinkerton

![](https://cloud.githubusercontent.com/assets/13026/3220006/68b7cc1a-effb-11e3-8386-64e34b9c54d8.png)

pinkerton is a standalone tool for working with Docker images.

Currently it can download images and checkout working copies using the same
local storage format that Docker uses.

```text
Usage:
  pinkerton pull [options] <image-url>
  pinkerton checkout [options] <id> <image-id>
  pinkerton cleanup [options] <id>
  pinkerton -h | --help

Commands:
  pull      Download a Docker image
  checkout  Checkout a working copy of an image
  cleanup   Destroy a working copy of an image

Examples:
  pinkerton pull https://registry.hub.docker.com/redis
  pinkerton pull https://registry.hub.docker.com/ubuntu?tag=trusty
  pinkerton pull https://registry.hub.docker.com/flynn/slugrunner?id=1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton checkout slugrunner-test 1443bd6a675b959693a1a4021d660bebbdbff688d00c65ff057c46702e4b8933
  pinkerton cleanup slugrunner-test

Options:
  -h, --help       show this message and exit
  --driver=<name>  storage driver [default: aufs]
  --root=<path>    storage root [default: /var/lib/docker]
```

## Roadmap

Future features might include:

- Repository/image/container introspection
- Building/editing of images
- Pushing images to Docker registries
- Making the UI more friendly to humans
- Parallel HTTP requests
- Pull resumption
