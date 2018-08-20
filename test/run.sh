#!/bin/bash

set -e

export HOME="/root"
export PATH="${ROOT}/build/bin:${PATH}"
export BACKOFF_PERIOD="5s"

main() {
  if [[ -n "${ROUTER_IP}" ]] && [[ -n "${DOMAIN}" ]]; then
    echo "${ROUTER_IP}" \
      "${DOMAIN}" \
      "controller.${DOMAIN}" \
      "git.${DOMAIN}" \
      "dashboard.${DOMAIN}" \
      "docker.${DOMAIN}" \
      >> /etc/hosts
  fi

  flynn cluster add --docker ${CLUSTER_ADD_ARGS}

  cd "${ROOT}/test"

  exec /bin/flynn-test \
    --flynnrc    "${HOME}/.flynnrc" \
    --cli        "$(readlink -f ../build/bin/flynn)" \
    --flynn-host "$(readlink -f ../build/bin/flynn-host)" \
    $@
}

main "$@"
