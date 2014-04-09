FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD start.sh /bin/start-flynn-receive
ADD build/flynn-receive /bin/flynn-receive
ADD build/gitreceived /bin/gitreceived
ADD build/sdutil /bin/sdutil

ENTRYPOINT ["/bin/start-flynn-receive"]
