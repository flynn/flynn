FROM flynn/busybox

ADD bin/flynn-bootstrap /bin/flynn-bootstrap
ADD bin/manifest.json /etc/manifest.json

ENTRYPOINT ["/bin/flynn-bootstrap"]
CMD ["/etc/manifest.json"]
