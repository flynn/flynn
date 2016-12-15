#!/bin/bash
#
# A script to install Docker 1.9.1 on Ubuntu 16.04.

set -eo pipefail

URL="https://get.docker.com/builds/Linux/x86_64/docker-1.9.1"
SHA="52286a92999f003e1129422e78be3e1049f963be1888afc3c9a99d5a9af04666"
DOCKER="/usr/local/bin/docker"

main() {
  if [[ -e "${DOCKER}" ]]; then
    exit
  fi

  download_docker
  add_docker_group
  install_systemd_service
}

download_docker() {
  echo "Downloading Docker 1.9.1 to ${DOCKER}..."
  local tmp="$(mktemp --directory)"
  trap "rm -rf ${tmp}" EXIT
  curl -fSLo "${tmp}/docker" "${URL}"
  echo "${SHA}  ${tmp}/docker" | sha256sum -c -
  sudo mv "${tmp}/docker" "${DOCKER}"
  sudo chmod +x "${DOCKER}"
}

add_docker_group() {
  echo "Adding Docker group"
  sudo groupadd docker
  sudo usermod -a -G docker "$(whoami)"
}

install_systemd_service() {
  echo "Installing Docker systemd unit"
  local root="$(cd "$(dirname "$0")" && pwd)"
  sudo cp "${root}/docker.socket"  "/lib/systemd/system/docker.socket"
  sudo systemctl enable docker.socket
  sudo cp "${root}/docker.service" "/lib/systemd/system/docker.service"
  sudo systemctl enable docker.service
  sudo systemctl start docker.service
}

main $@
