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
      "images.${DOMAIN}" \
      >> /etc/hosts
  fi

  flynn cluster add ${CLUSTER_ADD_ARGS}

  cd "${ROOT}/test"

  exec /bin/flynn-test $@
}

main "$@"
