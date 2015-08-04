#!/usr/bin/env bats

load "helper"
load_lib "tuf.sh"

setup() {
  TMPDIR="$(mktemp --directory)"
}

teardown() {
  rm -rf "${TMPDIR}"
}

create_metatdata_with_expires() {
  local path=$1
  local offset=$2
  local expires="$(date --date "${offset}" --utc +%Y-%m-%dT%H:%M:%SZ)"
  echo "{\"signed\":{\"expires\":\"${expires}\"}}" > "${path}"
}

@test "metadata_expires_before with future expires" {
  local path="${TMPDIR}/meta.json"
  create_metatdata_with_expires "${path}" "+3 day"
  run metadata_expires_before "${path}" "+2 day"
  assert_failure
}

@test "metadata_expires_before with past expires" {
  local path="${TMPDIR}/meta.json"
  create_metatdata_with_expires "${path}" "+1 day"
  run metadata_expires_before "${path}" "+2 day"
  assert_success
}
