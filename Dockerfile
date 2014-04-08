FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD build/flynn-controller /bin/flynn-controller
ADD build/flynn-scheduler /bin/flynn-scheduler
ADD start.sh /bin/start-flynn-controller

ENTRYPOINT ["/bin/start-flynn-controller"]
