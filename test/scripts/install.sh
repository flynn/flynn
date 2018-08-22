#!/bin/bash

# Start an instance with:
#
# gcloud compute --project flynn-sandbox images create ubuntu-1604-vmx \
#   --source-image-family ubuntu-1604-lts --source-image-project ubuntu-os-cloud \
#   --licenses "https://www.googleapis.com/compute/v1/projects/vm-options/global/licenses/enable-vmx"
#
# gcloud beta compute --project flynn-sandbox instances create flynn-ci-0 \
#   --zone us-central1-a \
#   --machine-type n1-highmem-96 \
#   --subnet default \
#   --maintenance-policy MIGRATE \
#   --service-account "715530988024-compute@developer.gserviceaccount.com" \
#   --scopes "https://www.googleapis.com/auth/devstorage.read_only","https://www.googleapis.com/auth/logging.write","https://www.googleapis.com/auth/monitoring.write","https://www.googleapis.com/auth/servicecontrol","https://www.googleapis.com/auth/service.management.readonly","https://www.googleapis.com/auth/trace.append" \
#   --min-cpu-platform "Intel Skylake" \
#   --tags "https-server" \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --local-ssd interface=NVME \
#   --image ubuntu-1604-vmx \
#   --boot-disk-size 1000 \
#   --boot-disk-type pd-ssd \
#   --boot-disk-device-name flynn-ci-0

set -e

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

main() {
  local dir="/opt/flynn-test"
  local build_dir="${dir}/build"

  info "mounting build directory"
  setup_disk

  info
  info "install finished!"
  info "you should now run test/scripts/setup.sh from a machine that has a local build of Flynn."
}

setup_disk() {
  mkdir -p "${build_dir}"

  if ! grep -qF nvme0 /proc/mdstat; then
    mdadm --create /dev/md127 --chunk=512 --level=0 --raid-devices=8 \
      /dev/nvme0n1 \
      /dev/nvme0n2 \
      /dev/nvme0n3 \
      /dev/nvme0n4 \
      /dev/nvme0n5 \
      /dev/nvme0n6 \
      /dev/nvme0n7 \
      /dev/nvme0n8
  fi

  if ! grep -qF "${build_dir}" /etc/fstab; then
    mkfs.ext4 -b 4096 -O ^has_journal -E stride=128,stripe-width=1024,lazy_itable_init=0,lazy_journal_init=0,discard -F /dev/md127
    echo UUID=`sudo blkid -s UUID -o value /dev/md127` "${build_dir}" ext4 noatime,nodiratime,nodiscard,defaults,nofail,nobarrier 0 2 >> /etc/fstab
  fi

  if ! mount | grep -q "${build_dir}"; then
    mount "${build_dir}"
  fi
}

main "$@"
