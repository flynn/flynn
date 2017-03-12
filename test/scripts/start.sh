#!/bin/bash

set -e

main() {
  local dir="/opt/flynn-test"
  local src_dir="${dir}/src/github.com/flynn/flynn"

  if test -s "${dir}/.credentials"; then
    . "${dir}/.credentials"
  else
    echo "missing credentials file: ${dir}/.credentials" >&2
    exit 1
  fi

  # trace commands, making sure this is *after* sourcing the credentials
  # file so they are not echoed into the log file
  set -x

  cd "${src_dir}"
  git fetch origin
  git checkout --force --quiet origin/master
  GOPATH="${dir}" go build -race -o test/bin/flynn-test-runner ./test/runner

  # run the cleanup script
  "${src_dir}/test/scripts/cleanup.sh"

  if ! test -f "${dir}/build/rootfs.img"; then
    "${src_dir}/test/rootfs/build.sh" "${dir}/build"
    chown -R "flynn-test:flynn-test" "${dir}/build"
  fi

  export TMPDIR="${dir}/build"

  cd "${src_dir}/test"
  exec "bin/flynn-test-runner" \
    --user     "flynn-test" \
    --rootfs   "${dir}/build/rootfs.img" \
    --kernel   "${dir}/build/vmlinuz" \
    --db       "${dir}/flynn-test.db" \
    --tls-dir  "${dir}/certs" \
    --domain   "ci.flynn.io" \
    --backups-dir "${dir}/backups" \
    --gist
}

main $@
