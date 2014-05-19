# Shelf [![Build Status](https://travis-ci.org/flynn/shelf.svg?branch=master)](https://travis-ci.org/flynn/shelf)

A simple, fast HTTP file service.

Shelf provides a simple HTTP interface for reading, writing, and deleting files. It's like a simpler S3. All operations fall under these three HTTP verbs:

 * PUT: write a file: `curl -X PUT -T /path/to/local/file http://shelfhost/path/to/remote/file`
 * GET: read a file: `curl http://shelfhost/path/to/remote/file`
 * DELETE: delete a file: `curl -X DELETE http://shelfhost/path/to/remote/file`

There are no directory indexes. Parent directories are automatically created. Right now, the files are stored on the filesystem (wherever you point it), but it's intended to provide a simple, pre-authenticated gateway to S3 and maybe other file storage systems in the near future.

## Usage

``` 
Usage: shelf -p <port> -s <storage-path>

  -p="8888": Port to listen on
  -s="/var/lib/shelf": Path to store files
```

## Flynn 

[Flynn](https://flynn.io) is a modular, open source Platform as a Service (PaaS). 

If you're new to Flynn, start [here](https://github.com/flynn/flynn).

### Status

Flynn is in active development and **currently unsuitable for production** use. 

Users are encouraged to experiment with Flynn but should assume there are stability, security, and performance weaknesses throughout the project. This warning will be removed when Flynn is ready for production use.

Please report bugs as issues on the appropriate repository. If you have a general question or don't know which repo to use, report them [here](https://github.com/flynn/flynn/issues).

## Contributing

We welcome and encourage community contributions to Flynn.

Since the project is still unstable, there are specific priorities for development. Pull requests that do not address these priorities will not be accepted until Flynn is production ready.

Please familiarize yourself with the [Contribution Guidelines](https://flynn.io/docs/contributing) and [Project Roadmap](https://flynn.io/docs/roadmap) before contributing.

There are many ways to help Flynn besides contributing code:

 - Fix bugs or file issues
 - Improve the [documentation](https://github.com/flynn/flynn.io) including this website
 - [Contribute](https://flynn.io/#sponsor) financially to support core development

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Prime Directive, Inc.
