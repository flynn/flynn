FROM ubuntu:xenial

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install --yes curl iproute2 qemu-kvm && \
    apt-get clean

RUN curl -fsSLo /usr/local/bin/jq https://github.com/stedolan/jq/releases/download/jq-1.5/jq-linux64 && \
    chmod +x /usr/local/bin/jq

ADD start.sh /bin/start-vm.sh
