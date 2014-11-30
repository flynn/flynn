FROM flynn/busybox

ADD https://raw.githubusercontent.com/bagder/ca-bundle/master/ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
ADD ./etcd /bin/etcd
ADD ./etcdctl /bin/etcdctl

ENTRYPOINT ["/bin/etcd"]
