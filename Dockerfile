FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD ./build/lorne /bin/lorne
ADD ./manifest.json /etc/lorne.json

ENTRYPOINT ["/bin/lorne", "-manifest", "/etc/lorne.json"]
