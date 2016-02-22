FROM ubuntu:trusty-20160217

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update &&\
    apt-get dist-upgrade -y &&\
    apt-get update &&\
    apt-get install -y curl &&\
    apt-get install -y -q redis-server &&\
    apt-get clean &&\
    apt-get autoremove -y &&\
    mkdir /data

ADD bin/flynn-redis /bin/flynn-redis
ADD bin/flynn-redis-api /bin/flynn-redis-api

ADD start.sh /bin/start-flynn-redis
ADD dump.sh /bin/dump-flynn-redis
ADD restore.sh /bin/restore-flynn-redis

ENTRYPOINT ["/bin/start-flynn-redis"]
