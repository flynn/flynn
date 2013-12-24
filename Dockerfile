FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD https://github.com/coreos/etcd/releases/download/v0.2.0-rc1/etcd-v0.2.0-rc1-Linux-x86_64.tar.gz /tmp/etcd.tar.gz

RUN cd /tmp && \
    gzip -d etcd.tar.gz && \
    tar xf etcd.tar && \
    mv etcd-v0.2.0-rc1-Linux-x86_64/etcd etcd-v0.2.0-rc1-Linux-x86_64/etcdctl /bin && \
    rm -rf etcd.tar etcd-v0.2.0-rc1-Linux-x86_64

ENTRYPOINT ["/bin/etcd"]
