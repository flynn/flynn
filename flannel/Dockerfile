FROM flynn/busybox:trusty-20160217

ADD bin/flanneld /bin/flanneld
ADD bin/flannel-wrapper /bin/flannel-wrapper

ENTRYPOINT ["/bin/flannel-wrapper"]
