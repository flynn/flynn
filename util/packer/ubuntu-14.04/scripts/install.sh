#!/bin/bash

set -xeo pipefail

export DEBIAN_FRONTEND=noninteractive

main() {
  stop_cron

  if virtualbox_build; then
    # run early to speed up subsequent steps
    fix_dns_resolution
  fi

  if vagrant_build; then
    setup_sudo
    install_vagrant_ssh_key
    install_nfs
    package_cleanup
  fi

  if vmware_build; then
    install_linux_headers
    install_vmware_guest_tools
  fi

  if virtualbox_build; then
    install_vbox_guest_additions
    change_hostname
  fi

  enable_cgroups
  create_groups
  add_apt_sources
  install_packages
  install_flynn
  disable_docker_auto_restart
  install_go
  apt_cleanup

  if vagrant_build; then
    net_cleanup
    compress
  fi
}

stop_cron() {
  # cron can run apt/dpkg commands that will disrupt our tasks
  service cron stop
}

virtualbox_build() {
  [[ "${PACKER_BUILDER_TYPE}" == "virtualbox-ovf" ]]
}

vmware_build() {
  [[ "${PACKER_BUILDER_TYPE}" == "vmware-iso" ]]
}

vagrant_build() {
  virtualbox_build || vmware_build
}

setup_sudo() {
  if [[ ! -f /etc/sudoers.d/vagrant ]]; then
    echo "%vagrant ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/vagrant
    chmod 0440 /etc/sudoers.d/vagrant
  fi
}

install_vagrant_ssh_key() {
  local pub="https://raw.github.com/mitchellh/vagrant/master/keys/vagrant.pub"
  if [[ ! -f /home/vagrant/.ssh/authorized_keys ]]; then
    mkdir /home/vagrant/.ssh
    chmod 700 /home/vagrant/.ssh
    wget ${pub} \
      -O /home/vagrant/.ssh/authorized_keys
    chmod 600 /home/vagrant/.ssh/authorized_keys
    chown -R vagrant /home/vagrant/.ssh
  fi
}

install_nfs() {
  apt-get install -y nfs-common
}

package_cleanup() {
  apt-get purge -y puppet byobu juju ruby
}

install_linux_headers() {
  apt-get install -y build-essential linux-headers-$(uname -r)
}

install_vmware_guest_tools() {
  cd /tmp
  mkdir -p /mnt/cdrom
  mount -o loop ~/linux.iso /mnt/cdrom
  tar zxf /mnt/cdrom/VMwareTools-*.tar.gz -C /tmp/
  /tmp/vmware-tools-distrib/vmware-install.pl -d
  rm /home/vagrant/linux.iso
  umount /mnt/cdrom
}

install_vbox_guest_additions() {
  local vbox_version="$(cat /home/vagrant/.vbox_version)"
  local vbox_iso="VBoxGuestAdditions_${vbox_version}.iso"

  apt-get install -y dkms
  mount -o loop "${vbox_iso}" /mnt
  yes | sh /mnt/VBoxLinuxAdditions.run || true
  umount /mnt
  rm "${vbox_iso}"
}

change_hostname() {
  local hostname="flynn"

  echo "${hostname}" > /etc/hostname
  echo "127.0.1.1 ${hostname}" >> /etc/hosts
  hostname -F /etc/hostname
}

fix_dns_resolution() {
  # Address issues some hosts experience with DNS latency.
  # See https://github.com/mitchellh/vagrant/issues/1172 for a detailed discussion of the problem.
  echo "options single-request-reopen" >> /etc/resolvconf/resolv.conf.d/base
  resolvconf -u
}

enable_cgroups() {
  perl -p -i -e \
    's/GRUB_CMDLINE_LINUX=""/GRUB_CMDLINE_LINUX="cgroup_enable=memory swapaccount=1"/g' \
    /etc/default/grub
  /usr/sbin/update-grub
}

create_groups() {
  groupadd docker
  groupadd fuse || true
  usermod -a -G docker,fuse "${SUDO_USER}"
}

add_apt_sources() {
  # docker
  apt-key adv --keyserver keyserver.ubuntu.com \
    --recv 36A1D7869245C8950F966E92D8576A8BA88D21E9
  echo deb https://get.docker.com/ubuntu docker main \
    > /etc/apt/sources.list.d/docker.list

  # tup
  apt-key adv --keyserver keyserver.ubuntu.com \
    --recv 27947298A222DFA46E207200B34FBCAA90EA7F4E
  echo deb http://ppa.launchpad.net/titanous/tup/ubuntu trusty main \
    > /etc/apt/sources.list.d/tup.list

  # zfs
  apt-key adv --keyserver keyserver.ubuntu.com \
    --recv E871F18B51E0147C77796AC81196BA81F6B0FC61
  echo deb http://ppa.launchpad.net/zfs-native/stable/ubuntu trusty main \
    > /etc/apt/sources.list.d/zfs.list

  apt-get update
}

install_packages() {
  local packages=(
    "aufs-tools"
    "btrfs-tools"
    "bzr"
    "curl"
    "git"
    "iptables"
    "libdevmapper-dev"
    "libvirt-bin"
    "libvirt-dev"
    "linux-image-extra-$(uname -r)"
    "lxc-docker"
    "make"
    "mercurial"
    "tup"
    "ubuntu-zfs"
    "vim-tiny"
  )

  apt-get install -y ${packages[@]}

  # make tup suid root so that we can build in chroots
  chmod ug+s /usr/bin/tup

  # give non-root users access to tup fuse mounts
  sed 's/#user_allow_other/user_allow_other/' -i /etc/fuse.conf
}

install_flynn() {
  local repo="${FLYNN_REPOSITORY:-"https://dl.flynn.io"}"

  local script="install-flynn"
  if [[ -n "${FLYNN_VERSION}" ]]; then
    script="${script}-${FLYNN_VERSION}"
  fi

  bash -es -- -r "${repo}" < <(curl -sL --fail "${repo}/${script}")
}

disable_docker_auto_restart() {
  sed -i 's/^#DOCKER_OPTS=.*/DOCKER_OPTS="-r=false"/' /etc/default/docker
}

install_go() {
  cd /tmp
  wget j.mp/godeb
  tar xvzf godeb
  ./godeb install 1.4.2
}

apt_cleanup() {
  echo "cleaning apt cache"
  apt-get autoremove -y
  apt-get clean

  echo "deleting old kernels"
  cur_kernel=$(uname -r | sed 's/-*[a-z]//g' | sed 's/-386//g')
  kernel_pkg="linux-(image|headers|ubuntu-modules|restricted-modules)"
  meta_pkg="${kernel_pkg}-(generic|i386|server|common|rt|xen|ec2|virtual)"
  apt-get purge -y $(dpkg -l \
    | egrep ${kernel_pkg} \
    | egrep -v "${cur_kernel}|${meta_pkg}" \
    | awk '{print $2}')
}

net_cleanup() {
  # Removing leftover leases and persistent rules
  echo "cleaning up dhcp leases"
  rm /var/lib/dhcp/*
}

compress() {
  # Zero out the free space to save space in the final image:
  echo "Zeroing device to make space..."
  dd if=/dev/zero of=/EMPTY bs=1M || true
  rm -f /EMPTY
}

main $@
