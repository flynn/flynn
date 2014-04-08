FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./build/shelf /bin/shelf

ENTRYPOINT ["/bin/shelf"]
