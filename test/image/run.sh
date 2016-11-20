#!/bin/bash

set -e

export HOME="/root"
export PATH="${ROOT}/cli/bin:${ROOT}/host/bin:${PATH}"
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

  # remove --flynnrc from $@
  # TODO: remove once the CI runner no longer passes --flynnrc
  local args=()
  while [[ -n "$1" ]]; do
    if [[ "$1" = "--flynnrc" ]]; then
      shift 2
      continue
    fi
    args+=("$1")
    shift
  done

  exec /bin/flynn-test --flynnrc "${HOME}/.flynnrc" ${args[@]}
}

main "$@"
