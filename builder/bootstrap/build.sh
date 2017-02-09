#!/bin/bash
#
# A script to build Flynn base images.

set -e

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat >&2 <<USAGE
usage: $0 NAME

Build Flynn image NAME (either "builder" or "ubuntu")
USAGE
}

main() {
  local name=$1
  if [[ -z "${name}" ]]; then
    usage
    exit 1
  fi

  case "${name}" in
    builder)
      build_builder
      ;;
    ubuntu)
      build_ubuntu "$2"
      ;;
    *)
      usage
      exit 1
  esac
}

build_builder() {
  local tmp="$(mktemp --directory)"
  trap "rm -rf ${tmp}" EXIT

  (
    info "building builder image"

    info "copying builder image binaries"
    cp -r "${ROOT}/builder/bin" "${tmp}/"

    info "copying builder image mksquashfs + shared libs"
    mkdir -p "${tmp}/usr/bin" "${tmp}/lib/x86_64-linux-gnu"
    cp "/usr/bin/mksquashfs" "${tmp}/usr/bin"
    cp "/lib/x86_64-linux-gnu/liblzo2.so.2" "${tmp}/lib/x86_64-linux-gnu"

    info "fixing builder image root permissions"
    chmod 755 "${tmp}"

    info "creating builder image"
  ) >&2

  create_image "${tmp}" "builder"
}

build_ubuntu() {
  local dist=$1
  if [[ -z "${dist}" ]]; then
    fail "missing ubuntu distribution"
  fi

  local tmp="$(mktemp --directory)"
  trap "sudo rm -rf ${tmp}" EXIT

  (
    info "building ubuntu ${dist} image"

    info "downloading ubuntu ${dist} cloud image"
    local url="https://partner-images.canonical.com/core/${dist}/current/ubuntu-${dist}-core-cloudimg-amd64-root.tar.gz"
    curl -fSLo "${tmp}/ubuntu.tar.gz" "${url}"

    info "extracting ubuntu ${dist} cloud image"
    mkdir -p "${tmp}/root"
    sudo tar xf "${tmp}/ubuntu.tar.gz" -C "${tmp}/root"

    info "setting up ubuntu ${dist} cloud image"
    sudo chroot "${tmp}/root" bash < "${ROOT}/builder/bootstrap/ubuntu-setup.sh"

    info "creating ubuntu ${dist} image"
  ) >&2

  create_image "${tmp}/root" "ubuntu-${dist}"
}

create_image() {
  local dir=$1
  local name=$2

  sudo mkdir -p "/var/lib/flynn/local"
  sudo chown "${SUDO_USER}:${SUDO_USER}" "/var/lib/flynn/local"
  sudo -E $(which go) run "${ROOT}/builder/bootstrap/artifact.go" "${dir}" "${name}"
}

main $@
