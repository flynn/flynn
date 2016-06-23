FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

RUN apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv EA312927 &&\
    echo "deb http://repo.mongodb.org/apt/ubuntu trusty/mongodb-org/3.2 multiverse" > /etc/apt/sources.list.d/mongodb-org-3.2.list &&\
    apt-get update &&\
    apt-get install -y sudo mongodb-org &&\
    apt-get clean &&\
    apt-get autoremove -y

ADD bin/flynn-mongodb /bin/flynn-mongodb
ADD bin/flynn-mongodb-api /bin/flynn-mongodb-api

ADD start.sh /bin/start-flynn-mongodb
ADD dump.sh /bin/dump-flynn-mongodb
ADD restore.sh /bin/restore-flynn-mongodb

ENTRYPOINT ["/bin/start-flynn-mongodb"]
