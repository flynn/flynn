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

  # remove some flags from $@
  # TODO: remove once the CI runner no longer passes these flags
  local args=(
    "--flynnrc"    "${HOME}/.flynnrc"
    "--cli"        "$(readlink -f ../build/bin/flynn)"
    "--flynn-host" "$(readlink -f ../build/bin/flynn-host)"
  )
  while [[ -n "$1" ]]; do
    if [[ "$1" = "--flynnrc" ]] || [[ "$1" = "--flynn-host" ]] || [[ "$1" = "--cli" ]]; then
      shift 2
      continue
    fi
    args+=("$1")
    shift
  done

  exec /bin/flynn-test ${args[@]}
}

main "$@"
