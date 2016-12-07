#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

main() {
  info "stopping Flynn cluster"
  "${ROOT}/script/kill-flynn"

  info "removing build dir"
  sudo rm -rf "${ROOT}/build"
}

main $@
