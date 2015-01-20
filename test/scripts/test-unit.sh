#!/bin/bash
set -exo pipefail

util/commit-validator/validate-dco

util/commit-validator/validate-gofmt

go test -race -cover ./...
