FROM ubuntu:trusty
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

RUN apt-get update && apt-get -qy install git && apt-get clean
ADD start.sh /bin/start-flynn-receiver
ADD bin/flynn-receiver /bin/flynn-receiver
ADD bin/flynn-key-check /bin/flynn-key-check
ADD bin/gitreceived /bin/gitreceived
ADD bin/sdutil /bin/sdutil

ENTRYPOINT ["/bin/start-flynn-receiver"]
