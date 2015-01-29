#!/bin/bash
#
# This script is run by the CI runner to collect debugging information
# which will be printed if any tests fail.

memwatch() {
  interests=("$@")

  echo -n $(date "+%H:%M:%S.%3N")
  for x in "${interests[@]}"; do
    echo -ne "\t${x}"
  done
  echo

  while true; do
    snap="$(ps -A -o rss -o cmd)" # downgrade to '-a' or leave it out entirely for confined scope
    echo -n $(date "+%H:%M:%S.%3N")
    for x in "${interests[@]}"; do
      mem="$(echo "$snap" | grep "$x" | grep -v "$0" | awk '{ SUM += $1 } END { print SUM+0 }')"
      echo -ne "\t${mem}"
    done
    echo

    sleep 1
  done
}

netdebug() {
  while true; do
    echo "===> $(date +%H:%M:%S.%3N)"
    trace ifconfig eth0
    trace route -n
    trace arp -a
    echo "==================="
    echo
    sleep 5
  done
}

trace() {
    local cmd=$@
    echo "+ ${cmd}"
    $cmd
}
