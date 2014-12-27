FROM flynn/busybox

ADD https://raw.githubusercontent.com/bagder/ca-bundle/master/ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
ADD https://github.com/coreos/etcd/releases/download/v2.0.0-rc.1/etcd-v2.0.0-rc.1-linux-amd64.tar.gz /tmp/etcd.tar.gz
RUN cd /tmp && \
    tar xzf etcd.tar.gz && \
    mv etcd-v2.0.0-rc.1-linux-amd64/etcd etcd-v2.0.0-rc.1-linux-amd64/etcdctl /bin && \
    rm -rf etcd.tar.gz etcd-v2.0.0-rc.1-linux-amd64

ENTRYPOINT ["/bin/etcd"]
