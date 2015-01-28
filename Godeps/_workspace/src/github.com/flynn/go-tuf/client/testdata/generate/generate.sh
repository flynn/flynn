#!/bin/bash
#
# A script to generate TUF repository files using the Python implementation.
#
# A list of generated files is printed to STDERR and a tar of the files to STDOUT.

set -e

main() {
  local dir="$(mktemp -d)"
  trap "rm -rf ${dir}" EXIT

  pushd "${dir}" >/dev/null
  generate_consistent
  generate_non_consistent
  list_files >&2
  tar c .
  popd >/dev/null
}

generate_consistent() {
  mkdir "with-consistent-snapshot"
  pushd "with-consistent-snapshot" >/dev/null
  /generate.py --consistent-snapshot
  popd >/dev/null
}

generate_non_consistent() {
  mkdir "without-consistent-snapshot"
  pushd "without-consistent-snapshot" >/dev/null
  /generate.py
  popd >/dev/null
}

list_files() {
  echo "Files generated:"
  tree
}

main $@
