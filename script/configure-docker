#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 CLUSTER_DOMAIN

Configure the local Docker daemon to trust a cluster's TLS CA certificate.
USAGE
}

main() {
  if [[ $# -ne 1 ]]; then
    usage
    exit 1
  fi

  CLUSTER_DOMAIN=$1

  local docker_cert_path="/etc/docker/certs.d/docker.${CLUSTER_DOMAIN}/ca.crt"
  sudo mkdir -p "$(dirname "${docker_cert_path}")"
  sudo curl --silent --output "${docker_cert_path}" "http://controller.${CLUSTER_DOMAIN}/ca-cert"

  source "/etc/lsb-release"
  if [[ "${DISTRIB_CODENAME}" = "xenial" ]]; then
    sudo systemctl restart docker
  else
    sudo restart docker
  fi
}

main $@
