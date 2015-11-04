#!/bin/bash
#
# A script to release the Flynn stable channel and build VM images.

set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"
source "${ROOT}/script/lib/aws.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options] VERSION

OPTIONS:
  -h            Show this message
  -b BUCKET     The S3 bucket to sync with [default: flynn]
  -d DOMAIN     The CloudFront domain [default: dl.flynn.io]
  -r DIR        Resume the release using DIR
  -t DIR        Path to the local TUF repository [default: /etc/flynn/tuf]

Requires AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY to be set
USAGE
}

main() {
  local bucket="flynn"
  local domain="dl.flynn.io"
  local tuf_dir="/etc/flynn/tuf"
  local dir

  while getopts "hb:d:r:t:" opt; do
    case $opt in
      h)
        usage
        exit 1
        ;;
      b) bucket=${OPTARG} ;;
      d) domain=${OPTARG} ;;
      r)
        dir=${OPTARG}
        if [[ ! -d "${dir}" ]]; then
          fail "No such directory: ${dir}"
        fi
        ;;
      t)
        tuf_dir=${OPTARG}
        if [[ ! -d "${tuf_dir}" ]]; then
          fail "No such directory: ${tuf_dir}"
        fi
        ;;
      ?)
        usage
        exit 1
        ;;
    esac
  done
  shift $((${OPTIND} - 1))

  if [[ $# -ne 1 ]]; then
    usage
    exit 1
  fi

  check_aws_keys

  local version=$1
  dir="${dir:-$(mktemp -d)}"

  info "updating the stable channel"
  "${ROOT}/script/release-channel" \
    --bucket "${bucket}" \
    --tuf-dir "${tuf_dir}" \
    "stable" \
    "${version}"

  info "releasing vm images"
  "${ROOT}/script/release-vm-images" \
    -k \
    -b "${bucket}" \
    -d "${domain}" \
    -r "${dir}" \
    "${version}"

  info "successfully released the stable channel to version ${version}"

  info "removing locally built files"
  rm -rf "${dir}"

  info "done!"
}

main $@
