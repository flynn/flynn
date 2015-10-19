#!/bin/bash

set -exo pipefail

tmpdir=$(mktemp --directory)

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 <image|app> <dashboard|installer>
USAGE
}

main() {
  local target pkg
  case $2 in
    dashboard)
      target=$2
      pkg="main"
      ;;
    installer)
      target=$2
      pkg=$2
      ;;
    *)
      echo "unknown target"
      exit 1
      ;;
  esac

  case $1 in
    image)
      image $target
      ;;
    app)
      app $target $pkg
      ;;
    *)
      echo "unknown command"
      exit 1
      ;;
  esac

  rm --recursive --force "${tmpdir}"
}

image() {
  local target
  target=$1

  cp ${ROOT}/util/assetbuilder/Dockerfile ${ROOT}/${target}/app/Gemfile* "${tmpdir}"
  docker build --tag flynn/${target}-builder "${tmpdir}"
}

app() {
  local target pkg dir
  target=$1
  pkg=$2
  dir=$(pwd)

  cp --recursive ${ROOT}/${target}/app/* "${tmpdir}"
  cp --recursive ${ROOT}/${target}/app/.eslintrc "${tmpdir}"
  cd "${tmpdir}"

  docker run \
    --volume "${tmpdir}:/build" \
    --workdir /build \
    flynn/${target}-builder \
    bash -c "cp --recursive /app/.bundle . && cp --recursive /app/vendor/bundle vendor/ && cp --recursive /app/node_modules . && ./compiler && chown -R $(id -u):$(id -g) ."

  rm -f app # {target}/app/compiler.go -> {target}/app/app binary
  mkdir app
  mv build app
  cd ${ROOT}/${target}
  ./bin/go-bindata -nomemcopy -nocompress -prefix ${tmpdir}/ -pkg ${pkg} ${tmpdir}/app/build/...
  cd ${dir}
}

main "$@"
