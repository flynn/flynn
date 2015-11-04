#!/usr/bin/env bats

load "helper"
load_lib "release.sh"

# override date so that it's predictable in tests.
DATE="20150301"
date() {
  echo "${DATE}"
}

@test "next_release_version with previous date in tag" {
  run next_release_version "20150101.0"

  assert_success
  assert_output "${DATE}.0"
}

@test "next_release_version with today's date in tag" {
  run next_release_version "${DATE}.0"

  assert_success
  assert_output "${DATE}.1"
}

@test "next_release_version can handle 2 digit iterations" {
  run next_release_version "${DATE}.9"

  assert_success
  assert_output "${DATE}.10"
}
