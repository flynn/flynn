FROM ubuntu:trusty-20160217

RUN apt-get update && apt-get -qy install git && apt-get clean

ADD bin/taffy /bin/taffy
ADD bin/flynn-receiver /bin/flynn-receiver

ENTRYPOINT ["/bin/taffy"]
