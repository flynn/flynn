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

`authchecker` is a path to an executable that will check if the push is authorized, and exit with status 0 if it is. It will be called with the following arguments:

    authchecker $USER $PATH $KEY

* `$USER` is the username that was provided to the server.
* `$PATH` is the path of the repo that was pushed to. It will not contain slashes.
* `$KEY` is the public key that was provided to the server.

The `receiver` is a path to an executable that will handle the push. It will get a tar stream of the repo via stdin and the following arguments:

    receiver $PATH $COMMIT

* `$PATH` is the path of the repo that was pushed to. It will not contain slashes.
* `$COMMIT` is the SHA of the commit that was pushed to master.

## TODO

* Write tests.
* Allow authchecker to return JSON including receiver environment.
* Support RPC as an option for authchecker.
