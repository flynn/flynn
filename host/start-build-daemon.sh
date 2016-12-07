#!/bin/bash
#
# A script to start flynn-host on IP 192.0.2.100 and using /var/lib/flynn/local
# (used by flynn-build to run jobs and save filesystem diffs).

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

BRIDGE="buildbr0"
IP="192.0.2.100"

# use a static network config so we don't need to start discoverd / flannel
NETWORK_CONFIG='{ "subnet": "198.51.100.1/24", "mtu": 1500 }'

main() {
  info "starting flynn-host build daemon"

  local dir="/var/lib/flynn/local"
  mkdir -p ${dir}/{bin,layers,log,volumes,tmp}
  chown "${SUDO_USER}" ${dir}/{layers,tmp}

  # stop the existing daemon
  start-stop-daemon \
    --stop \
    --oknodo \
    --retry 15 \
    --pidfile "${dir}/flynn-host.pid"

  # create an alias ethernet interface for flynn-host
  # to listen on
  ifconfig "eth0:build" "${IP}"

  # copy the binaries as we can't exec them from within
  # the tup FUSE mount, renaming flynn-host so it is not
  # killed by script/kill-flynn
  cp "${ROOT}/host/bin/flynn-host" "${dir}/bin/flynn-host-local"
  cp "${ROOT}/host/bin/flynn-init" "${dir}/bin/flynn-init"

  # start the daemon
  start-stop-daemon \
    --start \
    --background \
    --no-close \
    --pidfile "${dir}/flynn-host.pid" \
    --make-pidfile \
    --exec "${dir}/bin/flynn-host-local" \
    -- \
    daemon \
    --force \
    --id          "build" \
    --external-ip "${IP}" \
    --listen-ip   "${IP}" \
    --state       "${dir}/state.bolt" \
    --volpath     "${dir}/volumes" \
    --log-dir     "${dir}/log" \
    --flynn-init  "${dir}/bin/flynn-init" \
    --bridge-name "buildbr0" \
    --network-config "${NETWORK_CONFIG}" \
    --max-job-concurrency "100" \
    --no-discoverd \
    &> "${dir}/log/flynn-host.log"

  # wait for it to start
  for i in $(seq 100); do
    if curl -fs "http://${IP}:1113/host/status" &>/dev/null; then
      exit
    fi
    sleep 0.1
  done

  fail "flynn-host build daemon failed to start (check ${dir}/log/flynn-host.log)"
}

main $@
