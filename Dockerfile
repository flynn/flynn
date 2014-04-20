FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD build/flynn-bootstrap /bin/flynn-bootstrap
ADD bootstrapper/manifest.json /etc/manifest.json

ENTRYPOINT ["/bin/flynn-bootstrap"]
CMD ["/etc/manifest.json"]
