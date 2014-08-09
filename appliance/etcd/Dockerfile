FROM flynn/busybox

ADD https://github.com/coreos/etcd/releases/download/v0.4.6/etcd-v0.4.6-linux-amd64.tar.gz /tmp/etcd.tar.gz
RUN cd /tmp && \
    tar xzf etcd.tar.gz && \
    mv etcd-v0.4.6-linux-amd64/etcd etcd-v0.4.6-linux-amd64/etcdctl /bin && \
    rm -rf etcd.tar.gz etcd-v0.4.6-linux-amd64

ENTRYPOINT ["/bin/etcd"]
