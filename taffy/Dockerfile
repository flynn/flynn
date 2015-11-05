FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

# Try to use the Hosts squid-deb-proxy and fall back if it's unreachable
RUN (echo 'Acquire { Retries "0"; HTTP { Proxy "http://172.17.42.1:8000"; }; };' > /etc/apt/apt.conf.d/10_docker_hostproxy && apt-get update -qq 2>&1 | grep -q 'Unable to connect to 172.17.42.1:8000' && rm /etc/apt/apt.conf.d/10_docker_hostproxy) || true &&\
    apt-get update &&\
    apt-get dist-upgrade -y

RUN apt-get apt-get -qy install git && apt-get clean &&\
    rm /etc/apt/apt.conf.d/10_docker_hostproxy || true

ADD bin/taffy /bin/taffy
ADD bin/flynn-receiver /bin/flynn-receiver

ENTRYPOINT ["/bin/taffy"]
