#!/bin/bash
set -exo pipefail

util/commit-validator/validate-dco

util/commit-validator/validate-gofmt

export PGHOST=/var/run/postgresql
sudo service postgresql start

go test -race -cover ./...

sudo service postgresql stop
