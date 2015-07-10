#!/bin/bash
set -exo pipefail

util/commit-validator/validate-dco

util/commit-validator/validate-gofmt

bats script/test

export PGHOST=/var/run/postgresql
sudo service postgresql start

make test-unit-root

sudo service postgresql stop
