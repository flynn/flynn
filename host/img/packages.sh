#!/bin/bash

# install packages for starting flynn-host within an existing Flynn cluster
# either in a container or in a VM
export DEBIAN_FRONTEND=noninteractive
apt-get update
# explicitly install linux 4.13 as the version of ZFS available on xenial is
# not compatible with linux 4.15 (the 'zfs' command just hangs)
apt-get install --yes linux-image-4.13.0-1019-gcp initramfs-tools systemd udev zfsutils-linux iptables net-tools iproute2 qemu-kvm
apt-get clean

# support 9p rootfs when starting in a VM
printf '%s\n' 9p 9pnet 9pnet_virtio >> /etc/initramfs-tools/modules
update-initramfs -u

# install jq for reading container config files
JQ_URL="https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64"
JQ_SHA="af986793a515d500ab2d35f8d2aecd656e764504b789b66d7e1a0b727a124c44"
curl -fsSLo /tmp/jq "${JQ_URL}"
echo "${JQ_SHA}  /tmp/jq" | sha256sum -c -
mv /tmp/jq /usr/local/bin/jq
chmod +x /usr/local/bin/jq

# add a systemd service to start flynn-host in a VM
cat > /etc/systemd/system/flynn-host.service <<EOF
[Unit]
Description=Flynn host daemon
Documentation=https://flynn.io/docs
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/start-flynn-host.sh --systemd
Restart=on-failure

# set delegate yes so that systemd does not reset the cgroups of containers
Delegate=yes

# kill only the flynn-host process, not all processes in the cgroup
KillMode=process

# every container uses several fds, so make sure there are enough
LimitNOFILE=10000

[Install]
WantedBy=multi-user.target
EOF
systemctl enable flynn-host.service

# configure VM networking
cat > /etc/systemd/network/10-flynn.network <<EOF
[Match]
Name=en*

[Network]
DHCP=ipv4
EOF
systemctl enable systemd-networkd.service
