FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD etcd/bin/etcd /bin/etcd

ENTRYPOINT ["/bin/etcd"]
