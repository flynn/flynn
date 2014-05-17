# flynn-test runner rootfs

The scripts in this directory build an Ubuntu 14.04 rootfs image to be used with
User-mode Linux (see `../uml` for UML build tooling).

The image expects a network configuration to be provided via `hostfs`.

## Building

```text
make
```

The script depends on the `zerofree` package.

## Networking

Configure the host with a bridge and NAT:

```text
brctl addbr flynnbr0
ip addr add 192.168.50.1/24 dev flynnbr0
ip link set flynnbr0 up

echo 1 > /proc/sys/net/ipv4/ip_forward
iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
iptables -A FORWARD -i flynnbr0 -o eth0 -j ACCEPT
```

Create a TAP device for the VM:

```text
ip tuntap add dev flynntap0 mode tap user ubuntu
ip addr add 192.168.50.10/24 dev flynntap0
ip link set flynntap0 up
brctl addif flynnbr0 flynntap0
```

Create a directory with the network configuration for the VM:

```text
mkdir net
cat >net/eth0 <<EOF
auto eth0
iface eth0 inet static
  address 192.168.50.20
  gateway 192.168.50.1
  netmask 255.255.255.0
  dns-nameservers 8.8.8.8 8.8.4.4
EOF
```

## Booting

Given a user-mode `linux` binary:

```text
linux mem=512M ubd0=uml0.cow1,rootfs.img umid=uml0 con0=fd:0,fd:1 con=pts rw eth0=tuntap,flynntap0 hostfs=`pwd`/net
```

After the VM boots you can connect via SSH with `ssh -p 2222
ubuntu@192.168.50.20` (password: `ubuntu`) or use one of the consoles via screen
(logged to stdout, ex: `/dev/pts/6`).
