FROM flynn/busybox

ADD bin/flanneld /bin/flanneld
ADD bin/flannel-wrapper /bin/flannel-wrapper

ENTRYPOINT ["/bin/flannel-wrapper"]
