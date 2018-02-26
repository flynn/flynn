#!/bin/bash
#
# A script to update Flynn release channels.
#
# PREREQUISITES:
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
usage: $0 [options] CHANNEL VERSION

OPTIONS:
  -h, --help            Show this message
  -b, --bucket BUCKET   The S3 bucket to sync with [default: flynn]
  -d, --tuf-dir DIR     Path to the local TUF repository [default: /etc/flynn/tuf]
  -e, --edit            Edit the changelog before committing the channel update
  --no-sync             Don't sync files with S3
  --no-changelog        Don't create a changelog
USAGE
}

main() {
  local bucket="flynn"
  local tuf_dir="/etc/flynn/tuf"
  local changelog=true
  local edit=false
  local sync=true

  while true; do
    case "$1" in
      -h | --help)
        usage
        exit 1
        ;;
      -b | --bucket)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        fi
        bucket="$2"
        shift 2
        ;;
      -d | --tuf-dir)
        if [[ -z "$2" ]]; then
          fail "$1 requires an argument"
        fi
        tuf_dir="$2"
        shift 2
        ;;
      -e | --edit)
        edit=true
        shift
        ;;
      --no-sync)
        sync=false
        shift
        ;;
      --no-changelog)
        changelog=false
        shift
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

  if [[ ! -d "${tuf_dir}" ]]; then
    fail "TUF directory \"${tuf_dir}\" does not exist"
  fi

  check_tuf_keys "${tuf_dir}"

  local channel=$1
  local version=$2

  info "setting the ${channel} release channel to ${version}"

  if $sync; then
    check_aws_keys
    info "downloading existing TUF metadata"
    download_tuf_metadata "${tuf_dir}" "${bucket}"
  fi

  info "checking version ${version} has been released"
  if ! release_exists "${tuf_dir}" "${version}"; then
    fail "version ${version} has not been released"
  fi

  cd "${tuf_dir}"
  ${ROOT}/build/bin/tuf clean

  if $changelog; then
    info "generating changelog"
    local changelog_path="${tuf_dir}/staged/targets/channel/${channel}/${version}/CHANGELOG.md"
    mkdir -p "$(dirname "${changelog_path}")"
    generate_changelog

    if $edit; then
      info "starting vim to edit the changelog"
      edit_changelog
    fi
  fi

  info "staging updated channel file"
  mkdir -p "staged/targets/channels"
  echo "${version}" > "staged/targets/channels/${channel}"

  info "committing TUF repository"
  ${ROOT}/build/bin/tuf add
  ${ROOT}/build/bin/tuf snapshot
  ${ROOT}/build/bin/tuf timestamp
  ${ROOT}/build/bin/tuf commit

  if $sync; then
    info "uploading files to S3"
    local dir="$(mktemp --directory)"
    mkdir -p "${dir}/upload"
    ln -fs "${tuf_dir}/repository" "${dir}/upload/tuf"
    sync_cloudfront "${dir}/upload/" "s3://${bucket}/"
  fi

  info "successfully set ${channel} release channel to ${version}"
}

# generate_changelog gets the current channel version from the TUF repository
# then creates a changelog by listing all pull requests which have a merge
# commit contained in the version being released but not in the current
# version.
#
# Changelog entries are formatted like:
#
#   * DATE: MESSAGE (LINK TO PR)
#
# For example:
#
#   * 2016-06-27: script: Add --version and --channel flags to install-flynn ([#3003](https://github.com/flynn/flynn/pull/3003))
generate_changelog() {
  local repo_url="https://s3.amazonaws.com/${bucket}/tuf"
  info "getting current ${channel} version from ${repo_url}"
  local tmp="$(mktemp --directory)"
  trap "rm -rf ${tmp}" EXIT
  ${ROOT}/build/bin/tuf-client init --store "${tmp}/tuf.db" "${repo_url}" <<< "$(${ROOT}/build/bin/tuf -d "${tuf_dir}" root-keys)"
  local current="$(tuf-client get --store "${tmp}/tuf.db" "${repo_url}" "/channels/${channel}")"
  if [[ -z "${current}" ]]; then
    warn "could not determine current ${channel} version, using empty changelog"
    echo "n/a" > "${changelog_path}"
    return
  fi

  pushd "${ROOT}" >/dev/null

  info "fetching git tags"
  git fetch --tags

  info "formatting changes between ${current} and ${version}"
  format_changes_between "${current}" "${version}" > "${changelog_path}"

  popd >/dev/null
}

format_changes_between() {
  local current=$1
  local version=$2

  local page="1"
  while true; do
    local pulls="$(pull_requests "${page}")"

    if [[ $(jq length <<< "${pulls}") = "0" ]]; then
      return
    fi

    while read number sha merged_at url title; do
      if tag_contains "${current}" "${sha}"; then
        return
      fi

      if tag_contains "${version}" "${sha}"; then
        echo "* $(date --date "${merged_at}" +%Y-%m-%d): ${title} ([#${number}](${url}))"
      fi
    done < <(jq --raw-output '.[] | (.number | tostring) + " " + .merge_commit_sha + " " + .merged_at + " " + .html_url + " " + .title' <<< "${pulls}")

    page=$((page + 1))
  done
}

pull_requests() {
  local page=$1
  local url="https://api.github.com/repos/flynn/flynn/pulls?state=closed&sort=updated&direction=desc&page=${page}"

  curl -fsSL "${url}"
}

tag_contains() {
  local tag=$1
  local sha=$2

  git tag --contains "${sha}" | grep -q "${tag}"
}

edit_changelog() {
  local tmpdir="$(mktemp --directory)"
  trap "rm -rf ${tmpdir}" EXIT
  local tmp="${tmpdir}/CHANGELOG.md"

  cat <<EOF > "${tmp}"
# You are currently editing the generated changelog for releasing the
# ${channel} channel.
#
# Lines starting with '#' will be ignored, and an empty changelog aborts the
# release.
# -----------------------------------------------------------------------------
EOF
  cat "${changelog_path}" >> "${tmp}"

  vim "${tmp}"

  sed -i '/^#/d' "${tmp}"

  if [[ ! -s "${tmp}" ]]; then
    warn "aborting release due to empty changelog"
    exit 0
  fi

  mv "${tmp}" "${changelog_path}"
}

main $@
