FROM flynn/busybox

ADD bin/flynn-controller /bin/flynn-controller
ADD bin/flynn-scheduler /bin/flynn-scheduler
ADD start.sh /bin/start-flynn-controller

ENTRYPOINT ["/bin/start-flynn-controller"]
