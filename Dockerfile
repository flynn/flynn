FROM ubuntu:12.04
MAINTAINER Jonathan Rudenberg <jonathan@titanous.com>

ENV DEBIAN_FRONTEND noninteractive

RUN echo "#!/bin/sh\nexit 101" > /usr/sbin/policy-rc.d; chmod +x /usr/sbin/policy-rc.d &&\
    apt-get update &&\
    apt-get -qy --fix-missing --force-yes install language-pack-en &&\
    update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8 &&\
    dpkg-reconfigure locales &&\
    apt-get -y install curl &&\
    curl -s https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - &&\
    sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ precise-pgdg main" >> /etc/apt/sources.list.d/postgresql.list' &&\
    apt-get update &&\
    apt-get install -y -q postgresql-9.3 postgresql-server-dev-9.3 postgresql-contrib-9.3 build-essential &&\
    rm /usr/sbin/policy-rc.d &&\
    cd /tmp &&\
    curl -sL https://github.com/dimitri/pgextwlist/archive/master.tar.gz | tar xzf - &&\
    cd pgextwlist-master &&\
    make &&\
    mkdir -p `pg_config --pkglibdir`/plugins &&\
    mv pgextwlist.so `pg_config --pkglibdir`/plugins &&\
    apt-get remove --purge -y postgresql-server-dev-9.3 build-essential &&\
    apt-get autoremove --purge -y &&\
    apt-get clean

ADD postgresql.conf /etc/postgresql/9.3/main/postgresql.conf
ADD pg_hba.conf /etc/postgresql/9.3/main/pg_hba.conf
ADD bin/flynn-postgres /bin/flynn-postgres
ADD bin/flynn-postgres-api /bin/flynn-postgres-api
ADD start.sh /bin/start-flynn-postgres

ENTRYPOINT ["/bin/start-flynn-postgres"]
