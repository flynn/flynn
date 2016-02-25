FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update &&\
    apt-get install -y software-properties-common &&\
    apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 0xcbcb082a1bb943db &&\
    add-apt-repository 'deb http://mirrors.syringanetworks.net/mariadb/repo/10.1/ubuntu trusty main' &&\
    apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 1C4CBDCDCD2EFD2A &&\
    add-apt-repository 'deb http://repo.percona.com/apt trusty main' &&\
    apt-get update &&\
    apt-get install -y sudo &&\
    apt-get install -y mariadb-server percona-xtrabackup &&\
    apt-get clean &&\
    apt-get autoremove -y

ADD bin/flynn-mariadb /bin/flynn-mariadb
ADD bin/flynn-mariadb-api /bin/flynn-mariadb-api
ADD start.sh /bin/start-flynn-mariadb

ENTRYPOINT ["/bin/start-flynn-mariadb"]
