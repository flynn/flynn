#!/usr/bin/env bats

load "helper"
load_lib "release.sh"

@test "next_release_version with empty manifest" {
  run next_release_version <<< "$(new_release_manifest)"
  assert_success
  assert_output "${DATE}.0"
}

@test "next_release_version with previous dates in manifest" {
  run next_release_version <<-MANIFEST
{
  "versions": [
    { "version": "20150101.0", "commit": "0f4a636" }
  ]
}
MANIFEST

  assert_success
  assert_output "${DATE}.0"
}

@test "next_release_version with today's date in manifest" {
  run next_release_version <<-MANIFEST
{
  "versions": [
    { "version": "20150101.0", "commit": "0f4a636" },
    { "version": "${DATE}.0", "commit": "0f4a636" }
  ]
}
MANIFEST

  assert_success
  assert_output "${DATE}.1"
}

@test "next_release_version with today's date in manifest (multiple)" {
  run next_release_version <<-MANIFEST
{
  "versions": [
    { "version": "20150101.0", "commit": "0f4a636" },
    { "version": "${DATE}.0", "commit": "0f4a636" },
    { "version": "${DATE}.1", "commit": "0f4a636" },
    { "version": "${DATE}.2", "commit": "0f4a636" }
  ]
}
MANIFEST

  assert_success
  assert_output "${DATE}.3"
}
