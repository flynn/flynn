#!/bin/bash
#
# A script to update the timestamp of a Flynn TUF repository

set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"
source "${ROOT}/script/lib/aws.sh"
source "${ROOT}/script/lib/tuf.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options]

OPTIONS:
  -h            Show this message
  -b BUCKET     The S3 bucket containing the TUF repository [default: flynn]
  -d DIR        Path to the local TUF repository [default: /etc/flynn/tuf]

Requires AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY to be set
USAGE
}

main() {
  local bucket="flynn"
  local tuf_dir="/etc/flynn/tuf"

  while getopts "hb:d:" opt; do
    case $opt in
      h)
        usage
        exit 1
        ;;
      b) bucket="${OPTARG}" ;;
      d)
        tuf_dir="${OPTARG}"
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

  if [[ $# -ne 0 ]]; then
    usage
    exit 1
  fi

  info "downloading existing TUF metadata"
  download_tuf_metadata "${tuf_dir}" "${bucket}"

  cd "${tuf_dir}"

  info "checking snapshot expires"
  if metadata_expires_before "repository/snapshot.json" "+1 day 1 hour"; then
    info "snapshot expires soon, updating"
    ${ROOT}/build/bin/tuf snapshot
  fi

  info "updating timestamp"
  ${ROOT}/build/bin/tuf timestamp

  info "committing changes"
  ${ROOT}/build/bin/tuf commit

  info "uploading timestamp and latest snapshot to S3"
  local tmp="$(mktemp --directory)"
  trap "rm -rf ${tmp}" EXIT
  local latest_snapshot="$(ls -t1 ${tuf_dir}/repository/*.snapshot.json | head -1)"
  ln -nfs "${latest_snapshot}" "${tuf_dir}"/repository/{snapshot,timestamp}.json "${tmp}"
  sync_cloudfront "${tmp}/" "s3://${bucket}/tuf/"
}

main $@
