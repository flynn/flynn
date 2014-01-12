FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./build/flynn-controller /bin/flynn-controller

ENTRYPOINT ["/bin/flynn-controller"]
