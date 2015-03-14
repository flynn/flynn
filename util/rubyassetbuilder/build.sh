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

  cp ${ROOT}/util/rubyassetbuilder/Dockerfile ${ROOT}/${target}/app/Gemfile* "${tmpdir}"
  docker build --tag flynn/${target}-builder "${tmpdir}"
}

app() {
  local target pkg dir
  target=$1
  pkg=$2
  dir=$(pwd)

  cp --recursive ${ROOT}/${target}/app/* "${tmpdir}"
  cd "${tmpdir}"

  docker run \
    --volume "${tmpdir}:/build" \
    --workdir /build \
    --user $(id -u) \
    flynn/${target}-builder \
    bash -c "cp --recursive /app/.bundle . && cp --recursive /app/vendor/bundle vendor/ && bundle exec rake compile"

  mkdir app
  mv build app
  cd ${ROOT}/${target}
  ./bin/go-bindata -nomemcopy -prefix ${tmpdir}/ -pkg ${pkg} ${tmpdir}/app/build/...
  cd ${dir}
}

main "$@"
