FROM flynn/busybox
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ADD start.sh /bin/start-flynn-receive
ADD build/flynn-receive /bin/flynn-receive
ADD build/gitreceived /bin/gitreceived

ENTRYPOINT ["/bin/start-flynn-receive"]
