#!/bin/bash
#
# A script to wait for Docker to start listening for requests. Used in CI where tests
# intermittently fail with `dial unix /var/run/docker.sock: no such file or directory`.

main() {
  start=$(date +%s)
  while ! [[ -S /var/run/docker.sock ]]; do
    sleep 0.2
    elapsed=$(($(date +%s) - $start))
    if [[ $elapsed -gt 60 ]]; then
      echo "++ $(timestamp) Docker not started after 60s, dumping /var/log/upstart/docker.log"
      sudo cat /var/log/upstart/docker.log
      exit 1
    fi
  done
}

timestamp() {
  date "+%H:%M:%S.%3N"
}

main $@
