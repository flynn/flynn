#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options] APP

Update a Flynn system app.

OPTIONS:
  -h, --help               Show this message
USAGE
}

main() {
  if [[ $1 = "-h" ]] || [[ $1 = "--help" ]]; then
    usage
    exit 0
  fi

  if [[ $# -ne 1 ]]; then
    usage
    exit 1
  fi

  local app=$1

  local config="$(mktemp)"
  trap "rm -f ${config}" EXIT

  local image_name="flynn/${app}"
  local image_id="$(jq -r ".\"${image_name}\"" "${ROOT}/version.json")"
  if [[ -z "${image_id}" ]]; then
    fail "could not determine ID for image ${image_name} in version.json"
  fi

  alias flynn="${ROOT}/cli/bin/flynn"
  flynn -a "${app}" release show --json | jq -r 'del(.id)' > "${config}"
  flynn -a "${app}" release add --file "${config}" "https://dl.flynn.io/tuf?name=${image_name}&id=${image_id}"
}

main $@
