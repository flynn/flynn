FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update &&\
    apt-get install -y software-properties-common apt-transport-https &&\
    apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 \
      199369E5404BD5FC7D2FE43BCBCB082A1BB943DB \
      4D1BB29D63D98E422B2113B19334A25F8507EFA5 &&\
    add-apt-repository 'deb http://sfo1.mirrors.digitalocean.com/mariadb/repo/10.1/ubuntu trusty main' &&\
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
