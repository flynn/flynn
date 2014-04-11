FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD build/flynn-bootstrap /bin/flynn-bootstrap

ENTRYPOINT ["/bin/flynn-bootstrap"]
