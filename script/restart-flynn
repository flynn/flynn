#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"
source "${ROOT}/script/lib/util.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options]

Restarts the running Flynn cluster. Simulates a hard power cycle of all nodes.

Use the --size flag to boot a multi-node cluster, which will create a virtual
network interface for each node and bind all host network services to that
interface (i.e. flynn-host, discoverd, flannel and router)

OPTIONS:
  -h, --help               Show this message
  -s, --size=SIZE          Cluster size [default: 1]
USAGE
}

main() {
  local size="1"

  while true; do
    case "$1" in
      -h | --help)
        usage
        exit 0
        ;;
      -s | --size)
        if [[ -z "$2" ]]; then
          usage
          exit 1
        fi
        size="$2"
        shift 2
        ;;
      -v | --version)
        if [[ -z "$2" ]]; then
          usage
          exit 1
        fi
        version="$2"
        shift 2
        ;;
      *)
        break
        ;;
    esac
  done

  if [[ $# -ne 0 ]]; then
    usage
    exit 1
  fi

  # kill flynn first
  info "killing cluster"
  "${ROOT}/script/kill-flynn"

  # restart all of the flynn-host processes
  info "restarting ${size} node cluster"
  for index in $(seq 0 $((size - 1))); do
    "${ROOT}/script/start-flynn-host" "-k" "-z" "${index}"
  done
}

main $@
