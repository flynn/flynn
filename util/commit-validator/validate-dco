#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/.validate"

adds=$(validate_diff --numstat | awk '{ s += $1 } END { print s }')
dels=$(validate_diff --numstat | awk '{ s += $2 } END { print s }')

: ${adds:=0}
: ${dels:=0}

if [ ${adds} -eq 0 -a ${dels} -eq 0 ]; then
  echo '0 additions, 0 deletions; nothing to validate.'
elif [ ${adds} -le 1 -a ${dels} -le 1 ]; then
  echo 'DCO small patch exception applies.'
else
  dcoPrefix='Signed-off-by:'
  dcoRegex="^${dcoPrefix} ([^<]+) <([^<>@]+@[^<>]+)>$"
  commits=( $(validate_log --format='format:%H%n') )
  badCommits=()
  for commit in "${commits[@]}"; do
    if [ -z "$(git log -1 --format='format:' --name-status "${commit}")" ]; then
      # no content (ie. merge commit, etc)
      continue
    fi
    if ! git log -1 --format='format:%B' "${commit}" | grep -qE "${dcoRegex}"; then
      badCommits+=( "${commit}" )
    fi
  done
  if [ ${#badCommits[@]} -eq 0 ]; then
    echo "All commits are properly signed with the DCO!"
  else
    {
      echo "These commits do not have a proper '${dcoPrefix}' sign-off:"
      for commit in "${badCommits[@]}"; do
        echo " - ${commit}"
      done
      echo
      echo 'Please amend each commit to include a properly formatted DCO sign-off.'
      echo
      echo "Visit the following URL for information about the Developer's Certificate of Origin:"
      echo ' https://flynn.io/docs/contributing#developer’s-certificate-of-origin'
      echo
    } >&2
    false
  fi
fi
