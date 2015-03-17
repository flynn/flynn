FROM flynn/busybox

ADD https://raw.githubusercontent.com/bagder/ca-bundle/master/ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
ADD ./bin/etcd /bin/etcd
ADD ./bin/etcdctl /bin/etcdctl
ADD ./start.sh /bin/start-etcd

ENTRYPOINT ["/bin/start-etcd"]
