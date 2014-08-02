# flynn-test

flynn-test contains full-stack acceptance tests for Flynn.

## Usage

Currently flynn-test is not capable of bootstrapping the test environment, so
you'll need to get Flynn running in [flynn-devbox](https://github.com/flynn/flynn-devbox).

After you have bootstrapped Flynn in flynn-devbox, clone this repository, copy
the `flynn server-add` command and use it to create a local flynnrc:

```text
FLYNNRC=`pwd`/flynnrc flynn server-add ...
```

Now you can build and run the tests:

```text
godep go build
./flynn-test -debug -flynnrc `pwd`/flynnrc
```
