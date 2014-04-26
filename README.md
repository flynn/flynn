# gitreceived

An SSH server made specifically for accepting git pushes that will trigger an auth script and then a receiver script to handle the push.

This is a more advanced, standalone version of [gitreceive](https://github.com/progrium/gitreceive).

## Using gitreceived

```
Usage:  ./gitreceived [options] <privatekey> <authchecker> <receiver>

  -n=false: disable client authentication
  -p="22": port to listen on
  -r="/tmp/repos": path to repo cache
```

`privatekey` is the path to the server's private key (unencrypted).

`authchecker` is a path to an executable that will check if the key is authorized, and exit with status 0 if it is. It will be called with the following arguments:

    authchecker $USER $KEY

* `$USER` is the username that was provided to the server.
* `$KEY` is the public key that was provided to the server.

The `receiver` is a path to an executable that will handle the push. It will get a tar stream of the repo via stdin and the following arguments:

    receiver $PATH $COMMIT

* `$PATH` is the path of the repo that was pushed to. It will not contain slashes.
* `$COMMIT` is the SHA of the commit that was pushed to master.

## TODO

* Write tests.
* Allow authchecker to return JSON including receiver environment.
* Support RPC as an option for authchecker.

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

Flynn is a [trademark](https://flynn.io/docs/trademark-guidelines) of Apollic Software, LLC.
