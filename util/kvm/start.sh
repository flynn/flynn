#!/bin/bash

main() {
  local bridge="$(jq --raw-output '.Bridge' /.containerconfig)"
  if [[ -z "${bridge}" ]]; then
    fail "Bridge not found in /.containerconfig"
  fi

  local mac="$(jq --raw-output '.MAC' /.containerconfig)"
  if [[ -z "${mac}" ]]; then
    fail "MAC not found in /.containerconfig"
  fi

  # parse the disks from argv until we hit '--'
  local args=()
  local index=0
  while true; do
    if [[ "$1" = "--" ]]; then
      shift
      break
    fi
    args+=(
      -drive "file=$1,index=${index},media=disk"
    )
    index=$((index+1))
    shift
  done

  cat > /etc/qemu-ifup <<EOF
#!/bin/bash
ip link set \$1 up
ip link set \$1 master "${bridge}"
EOF

  args+=(
    -enable-kvm
    -device "e1000,netdev=net0,mac=${mac}"
    -netdev "tap,id=net0"
    -nographic
  )

  exec /usr/bin/qemu-system-x86_64 ${args[@]} $@
}

fail() {
  local msg=$1
  echo "ERROR: ${msg}" >&2
  exit 1
}

main $@
