# (Heroku-ish) Slug Builder

A tool that uses [Buildpacks](https://devcenter.heroku.com/articles/buildpacks)
to produce a Heroku-like
[slug](https://devcenter.heroku.com/articles/slug-compiler) given some
application source.

## What does it do exactly?

It's a shell script that takes an uncompressed tarball of an application source
piped to it. The source is run through buildpacks, then if it's detected as
a supported app it will be compiled into a gzipped tarball ready to be run
somewhere.

## Using Slug Builder

First, you need Docker. Then you can either pull the image from the public
index:

	$ docker pull flynn/slugbuilder

Or you can build from this source:

	$ docker build -t flynn/slugbuilder .

When you run the container, it always expects a tar of your app source to be
passed via stdin. So let's run it from a git repo and use `git archive` to
produce a tar:

	$ id=$(git archive master | docker run -i -a stdin flynn/slugbuilder)
	$ docker wait $id
	$ docker cp $id:/tmp/slug.tgz .

We run slugbuilder, wait for it to finish using the id it gave us, then copies
out the slug artifact into the current directory. If we attached to the
container with `docker attach` we could also see the build output as you would
with Heroku. We can also *just* see the build output by running it with stdout:

	$ git archive master | docker run -i -a stdin -a stdout flynn/slugbuilder

We still have to look up the id and copy the slug out of the container, but
there's an easier way!

	$ git archive master | docker run -i -a stdin -a stdout flynn/slugbuilder - > myslug.tgz

By running with the `-` argument, it will send all build output to stderr (which
we didn't attach here) and then spit out the slug to stdout, which as you can
see we can easily redirect into a file.

Lastly, you can also have it PUT the slug somewhere via HTTP if you give it
a URL as an argument. This lets us specify a place to put it *and* get the build
output via stdout:

	$ git archive master | docker run -i -a stdin -a stdout flynn/slugbuilder http://fileserver/path/for/myslug.tgz

## Caching

To speed up slug building, it's best to mount a volume specific to your app at
`/tmp/cache`. For example, if you wanted to keep the cache for this app on your
host at `/tmp/app-cache`, you'd mount a read-write volume by running docker with
this added `-v /tmp/app-cache:/tmp/cache:rw` option:

	docker run -v /tmp/app-cache:/tmp/cache:rw -i -a stdin -a stdout flynn/slugbuilder


## Buildpacks

As you can see, slugbuilder supports a number of official and third-party Heroku
buildpacks. You can change the buildpacks.txt file and rebuild the container to
create a version that supports more/less buildpacks than we do here. You can
also bind mount your own directory of buildpacks if you'd like:

	docker run -v /my/buildpacks:/tmp/buildpacks:ro -i -a stdin -a stdout flynn/slugbuilder

## Base Environment

The container image is based on [cedarish](/util/cedarish), an image that
emulates the Heroku Cedar stack environment. All buildpacks should have
everything they need to run in this environment, but if something is missing it
should be added to cedarish.
