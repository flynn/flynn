#!/bin/bash
#
# A script to build and release Flynn components.
#
# PREREQUISITES:
#
# - Install up-to-date s3cmd so CloudFront invalidation works:
#   sudo apt-get install -y python-dateutil
#   wget -O /tmp/s3cmd.deb http://archive.ubuntu.com/ubuntu/pool/universe/s/s3cmd/s3cmd_1.5.0~rc1-2_all.deb
#   sudo dpkg -i /tmp/s3cmd.deb
#   rm /tmp/s3cmd.deb
#
# - Configure s3cmd
#   s3cmd --configure
#
# - Set the TUF passphrases
#   export TUF_TARGETS_PASSPHRASE=xxxxxx
#   export TUF_SNAPSHOT_PASSPHRASE=xxxxxx
#   export TUF_TIMESTAMP_PASSPHRASE=xxxxxx

set -eo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"
source "${ROOT}/script/lib/aws.sh"
source "${ROOT}/script/lib/tuf.sh"

usage() {
  cat <<USAGE >&2
usage: $0 [options] COMMIT VERSION

OPTIONS:
  -h, --help              Show this message
  -c, --channel CHANNEL   Point channel at VERSION once released
  -b, --bucket BUCKET     The S3 bucket to sync with [default: flynn]
  -t, --tuf-dir DIR       Path to the local TUF repository [default: /etc/flynn/tuf]
  -k, --keep              Keep release directory
  -r, --resume DIR        Resume the release using DIR
USAGE
}

main() {
  local channel=""
  local bucket="flynn"
  local tuf_dir="/etc/flynn/tuf"
  local dir=""
  local keep=false

  while true; do
    case "$1" in
      -h | --help)
        usage
        exit 0
        ;;
      -c | --channel)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        fi
        channel="$2"
        shift 2
        ;;
      -b | --bucket)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        fi
        bucket="$2"
        shift 2
        ;;
      -t | --tuf-dir)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        elif [[ ! -d "$2" ]]; then
          fail "No such directory: $2"
        fi
        tuf_dir="$2"
        shift 2
        ;;
      -k | --keep)
        keep=true
        shift
        ;;
      -r | --resume)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        elif [[ ! -d "$2" ]]; then
          fail "No such directory: $2"
        fi
        dir="$2"
        shift 2
        ;;
      *)
        break
        ;;
    esac
  done

  if [[ $# -ne 2 ]]; then
    usage
    exit 1
  fi

  local commit=$1
  local version=$2
  local flynn_release="${ROOT}/util/release/flynn-release"

  dir="${dir:-$(mktemp --directory)}"
  info "using base dir: ${dir}"

  check_tuf_keys "${tuf_dir}"

  export GOPATH="${dir}"
  local src="${GOPATH}/src/github.com/flynn/flynn"

  if [[ ! -d "${src}/.git" ]]; then
    info "cloning git repo"
    rm -rf "${src}"
    git clone --quiet https://github.com/flynn/flynn "${src}"
  fi

  pushd "${src}" >/dev/null

  info "building flynn"
  git checkout --force --quiet "${commit}"

  make release

  popd >/dev/null

  info "downloading existing TUF metadata"
  download_tuf_metadata "${tuf_dir}" "${bucket}"

  info "adding components to the TUF repository"
  "${src}/script/export-components" "${tuf_dir}"

  if [[ -n "${channel}" ]]; then
    "${src}/script/release-channel" \
      --bucket "${bucket}" \
      --tuf-dir "${tuf_dir}" \
      --no-sync \
      "${channel}" \
      "${version}"
  fi

  info "uploading files to S3"
  mkdir -p "${dir}/upload"
  ln -fs "${src}/script/install-flynn" "${dir}/upload/install-flynn"
  ln -fs "${src}/script/install-flynn" "${dir}/upload/install-flynn-${version}"
  ln -fs "${tuf_dir}/repository" "${dir}/upload/tuf"
  sync_cloudfront "${dir}/upload/" "s3://${bucket}/"

  info "successfully released components for version ${version}"

  if $keep; then
    info "locally built packages will remain in ${dir}"
  else
    info "removing locally built packages"
    rm -rf "${dir}"
  fi
}

main $@
