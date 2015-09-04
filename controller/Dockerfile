FROM busybox

ADD bin/flynn-controller /bin/flynn-controller
ADD bin/flynn-scheduler /bin/flynn-scheduler
ADD bin/flynn-worker /bin/flynn-worker
ADD start.sh /bin/start-flynn-controller
ADD bin/jsonschema /etc/flynn-controller/jsonschema

ENTRYPOINT ["/bin/start-flynn-controller"]
