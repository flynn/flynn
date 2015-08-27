#!/bin/bash

cache="$2"
file="${cache}/a"
i=$(cat "${file}")

if [[ -n "${i}" ]]; then
  echo "cached: ${i}"
  ((i++))
  echo "${i}" > "${file}"
else
  echo 0 > "${file}"
fi

# Ensure there are no regressions for #1757, directories missing the +x bit
mkdir $1/foo/bar || exit 1
