dashboardv2
===========

## Development

### Generate protobuf files

The contents of `dashboardv2/src/generated` are updated via `make` and will
primarily change in response to editing `controller/api/controller.proto`.

```
(github.com/flynn/flynn) $ vagrant up && vagrant ssh
$ make
```

### Run dev server

```
(github.com/flynn/flynn) $ vagrant up && vagrant ssh
$ make
$ ./script/bootstrap-flynn

# this will output something like the following `flynn cluster add` line,
# copy the controller key (c17454354a80cc8095103392af80bf6d in this case)
# to use in `yarn start` below.

flynn cluster add -p 4DsZS2Koz7nv/lQwlgqvWWEHwJB2TWk93gd1vxEwXT0=\
default 1.localflynn.com c17454354a80cc8095103392af80bf6d
```

```
# on your local machine
(github.com/flynn/flynn) $ cd dashboardv2
$ yarn
$ CONTROLLER_KEY=c17454354a80cc8095103392af80bf6d yarn start
```

