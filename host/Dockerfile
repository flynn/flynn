FROM ubuntu:xenial

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install --yes zfsutils-linux iptables udev net-tools iproute2 && \
    apt-get clean

ADD bin/ca-certs.pem /etc/ssl/certs/ca-certs.pem
ADD bin/flynn-host   /usr/local/bin/flynn-host
ADD bin/flynn-init   /usr/local/bin/flynn-init
ADD zfs-mknod.sh     /usr/local/bin/zfs-mknod
ADD udev.rules       /lib/udev/rules.d/10-local.rules
ADD start.sh         /usr/local/bin/start-flynn-host.sh

CMD ["/usr/local/bin/start-flynn-host.sh"]
