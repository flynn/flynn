FROM ubuntu:12.04
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

RUN apt-get update && apt-get -qy install git && apt-get clean
ADD start.sh /bin/start-flynn-receive
ADD build/flynn-receive /bin/flynn-receive
ADD build/gitreceived /bin/gitreceived
ADD build/sdutil /bin/sdutil

ENTRYPOINT ["/bin/start-flynn-receive"]
