FROM ubuntu-debootstrap:14.04

RUN apt-get update && apt-get -qy install git && apt-get clean

ADD taffy.sh /bin/taffy
ADD bin/flynn-receiver /bin/flynn-receiver

ENTRYPOINT ["/bin/taffy"]
