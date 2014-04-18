FROM ubuntu:12.04
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

RUN apt-get update && apt-get -qy install git && apt-get clean

ADD taffy.sh /bin/taffy
ADD build/flynn-receive /bin/flynn-receive

ENTRYPOINT ["/bin/taffy"]
