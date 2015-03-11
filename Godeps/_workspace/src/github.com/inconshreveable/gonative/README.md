# gonative

Cross compiled Go binaries are not suitable for production applications
because code in the standard library relies on Cgo for DNS resolution
with the native resolver, access to system certificate roots, and parts of os/user.

gonative is a simple tool which creates a build of Go that can cross compile
to all platforms while still using the Cgo-enabled versions of the stdlib
packages. It does this by downloading the binary distributions for each
platform and copying their libraries into the proper places. It sets
the correct mod time so they don't get rebuilt. It also copies
some auto-generated runtime files into the build as well. gonative does
not modify any Go that you have installed and builds a new installaion of 
Go in a separate directory (the current directory by default).

Once you have a toolchain for cross-compilation, you can use tools like
[gox](https://github.com/mitchellh/gox) to cross-compile native builds easily.

gonative will not help you if your own packages rely on Cgo

### Installation

    git clone https://github.com/inconshreveable/gonative
    cd gonative
    make

Alternatively, you can install gonative via `go get` but the dependencies are not
locked down.

    go get github.com/inconshreveable/gonative

### Running
The 'build' command will build a toolchain in a directory called 'go' in your working directory.

    gonative build

To build a particular version of Go (default is 1.4):

    gonative build -version=1.3.3

For options and help:

    gonative build -h

### How it works

gonative downloads the go source code and compiles it for your host platform.
It then bootstraps the toolchain for all target platforms (but does not compile the standard library).
Then, it fetches the official binary distributions for all target platforms and copies
each pkg/OS\_ARCH directory into the toolchain so that you will link with natively-compiled versions
of the standard library. It walks all of the copied standard library and sets their modtimes so that
they won't get rebuilt. It also copies some necessary auto-generated runtime source
files for each platform (z\*\_) into the source directory to make it all work.

### Example with gox:

Here's an example of how to cross-compile a project:

    $ go get github.com/mitchellh/gox
    $ go get github.com/inconshreveable/gonative
    $ cd /your/project
    $ gonative build
    $ PATH=$PWD/go/bin/:$PATH gox
    
This isn't the most optimal way of doing things though. You only ever need one gonative-built 
Go toolchain. And with the proper GOPATH set up, you don't need to be
in your project's working directory. I use it mostly like this:

#### One time only setup:

    $ go get github.com/mitchellh/gox
    $ go get github.com/inconshreveable/gonative
    $ mkdir -p /usr/local/gonative
    $ cd /usr/local/gonative
    $ gonative build
    
#### Building a project:

    $ PATH=/usr/local/gonative/go/bin/:$PATH gox github.com/your-name/application-name
    
### Open Issues

- gonative is untested on Windows

### Caveats
- no linux/arm support because there are no official builds of linux/arm
- linux\_386 binaries that use native libs depend on 32-bit libc/libpthread/elf loader. some 64-bit linux distributions might not have those installed by default
