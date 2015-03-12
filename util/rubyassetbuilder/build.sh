#!/bin/bash

set -exo pipefail

tmpdir=$(mktemp --directory)

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

usage() {
  cat <<USAGE >&2
usage: $0 <image|app>
USAGE
}

main() {
  case $1 in
    image)
      image
      ;;
    app)
      app
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
  target="dashboard"

  cp ${ROOT}/util/rubyassetbuilder/Dockerfile ${ROOT}/${target}/app/Gemfile* "${tmpdir}"
  docker build --tag flynn/${target}-builder "${tmpdir}"
}

app() {
  local target dir
  target="dashboard"
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
  ./bin/go-bindata -nomemcopy -prefix ${tmpdir}/ -pkg main ${tmpdir}/app/build/...
  cd ${dir}
}

main "$@"
