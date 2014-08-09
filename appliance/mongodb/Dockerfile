FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

RUN apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv 7F0CEB10 &&\
    echo 'deb http://downloads-distro.mongodb.org/repo/ubuntu-upstart dist 10gen' > /etc/apt/sources.list.d/mongodb.list &&\
    apt-get update &&\
    apt-get dist-upgrade -y &&\
    apt-get install mongodb-org -y

ADD bin/flynn-mongodb /bin/flynn-mongodb
ADD start.sh /bin/start-flynn-mongodb

ENTRYPOINT ["/bin/start-flynn-mongodb"]
