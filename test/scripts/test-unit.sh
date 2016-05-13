#!/bin/bash
set -exo pipefail

util/commit-validator/validate-dco

util/commit-validator/validate-gofmt

bats script/test

GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup discoverd
PATH=$PATH:$PWD/discoverd/bin

export PGHOST=/var/run/postgresql
sudo service postgresql start

go test -race -cover ./...
sudo -E go test -race -cover ./host/volume/zfs ./pinkerton # these tests skip unless root

sudo service postgresql stop
