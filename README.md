# (Heroku-ish) Slug Builder
A tool using [Docker](http://docker.io) and [Buildpacks](https://devcenter.heroku.com/articles/buildpacks) to produce a Heroku-like [slug](https://devcenter.heroku.com/articles/slug-compiler) given some application source.

## Using Slug Builder

First, you need Docker. You can either pull the image from the public index:

	$ docker pull flynn/slugbuilder

Or you can build from this source:

	$ cd slugbuilder
	$ docker build -t flynn/slugbuilder .

Now there are two modes to run slugbuilder that change how you get out the slug artifact (a gzipped tarball). Both require you to pipe a tar of the source into STDIN. Here is the simplest way to use slugbuilder, assuming you're sitting in a git repo you want to build:

	$ git archive master | docker run -i -a stdin -a stdout flynn/slugbuilder stream > myslug.tgz

This is stream mode, where it will stream the slug out to STDOUT. All build output is sent to STDERR, which you can view if you attach to it by adding `-a stderr`. The second mode is file mode, which you can use two ways:

	$ id=$(git archive master | docker run -i -a stdin flynn/slugbuilder file)
	$ docker wait $id
	$ docker cp $id:/tmp/slug.tgz .

This runs slugbuilder, then waits for it to finish, then copies out the slug artifact into the current directory. This mode will output the build output to STDOUT and STDERR as appropriate, so if you want you can attach to it as it runs: 

	$ git archive master | docker run -i -a stdin -a stdout -a stderr flynn/slugbuilder file

Of course, now you don't have the ID of the container to run `docker cp` with, but you could look it up with `docker ps -a`, or you could run as before and attach afterwards. Lots of options. However, this mode is great for one final reason: you can upload via HTTP PUT if you provide a URL.

	$ git archive master | docker run -i -a stdin -a stdout -a stderr flynn/slugbuilder file http://fileserver/path/for/myslug.tgz

Attaching to STDOUT and STDERR is optional, but this lets you see the build output as it runs. 

## Buildpacks

As you can see, slugbuilder supports a number of official and third-party Heroku buildpacks. You can change the buildpacks.txt file and rebuild the image to create a version that supports more/less buildpacks than we do here. You can also bind mount your own directory of buildpacks if you'd like. 

## Caching

To speed up slug building, it's best to mount a volume specific to your app at `/tmp/cache`.

## Base Environment

The Docker image here is based on [cedarish](https://github.com/progrium/cedarish), an image that emulates the Heroku Cedar stack environment. All buildpacks should have everything they need to run in this environment, but if something is missing it should be added upstream to cedarish.

## License

BSD