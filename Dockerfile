FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD etcd/build/etcd /bin/etcd

ENTRYPOINT ["/bin/etcd"]
