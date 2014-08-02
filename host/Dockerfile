FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./build/flynn-host /bin/flynn-host
ADD ./manifest.json /etc/flynn-host.json

ENTRYPOINT ["/bin/flynn-host"]
