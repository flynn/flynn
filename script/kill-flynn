#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options]

Kill running flynn-host daemons.

OPTIONS:
  -h, --help            Show this message
USAGE
}

main() {
  if [[ $1 = "-h" ]] || [[ $1 = "--help" ]]; then
    usage
    exit 0
  fi

  if [[ $# -ne 0 ]]; then
    usage
    exit 1
  fi

  info "killing running flynn-host, if any"
  sudo start-stop-daemon \
    --stop \
    --oknodo \
    --retry 15 \
    --name "flynn-host"

  # force kill any remaining containers
  sudo start-stop-daemon \
    --stop \
    --oknodo \
    --retry 15 \
    --name ".containerinit"

  info "done!"
}

main $@
