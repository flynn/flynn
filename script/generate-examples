#!/bin/bash
#
# A script to generate controller examples to STDOUT.

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 <controller|router> [options]

OPTIONS:
  -h            Show this message
  -p PORT       The port to start the resource server on [default: 12345]
USAGE
}

main() {
  local ip port component

  case $1 in
    controller) component=$1 ;;
    router)  component=$1 ;;
    *)
      usage
      echo "Invalid component: $1"
      exit 2
      ;;
  esac

  while getopts 'hi:p:' opt; do
    case "${opt}" in
      h)
        usage
        exit 1
        ;;
      i) ip="${OPTARG}" ;;
      p) port="${OPTARG}" ;;
      c) component="${OPTARG}" ;;
      ?)
        usage
        exit 2
        ;;
    esac
  done

  port="${port:-"12345"}"

  export CONTROLLER_KEY="s3cr3t"

  info "bootstrapping flynn" >&2
  cluster_add=$("${ROOT}/script/bootstrap-flynn" &> >(tee /dev/stderr) | grep "flynn cluster add" | tail -1)
  if [[ "${cluster_add:0:17}" != "flynn cluster add" ]]; then
    fail "Bootstrap failed"
  fi

  info "generating ${component} examples..." >&2

  ${ROOT}/host/bin/flynn-host run \
    --bind="${ROOT}/docs/api-examples:/data" \
    "${ROOT}/build/image/${component}-examples.json" \
    sh -c "CONTROLLER_KEY=${CONTROLLER_KEY} PORT=${port} /bin/flynn-${component}-examples /data/${component}.json"
}

main $@
