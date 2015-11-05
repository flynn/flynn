FROM ubuntu-debootstrap:14.04

ENV DEBIAN_FRONTEND noninteractive

# Try to use the Hosts squid-deb-proxy and fall back if it's unreachable
RUN (echo 'Acquire { Retries "0"; HTTP { Proxy "http://172.17.42.1:8000"; }; };' > /etc/apt/apt.conf.d/10_docker_hostproxy && apt-get update -qq 2>&1 | grep -q 'Unable to connect to 172.17.42.1:8000' && rm /etc/apt/apt.conf.d/10_docker_hostproxy) || true &&\
    apt-get update &&\
    apt-get dist-upgrade -y

RUN apt-get -qy --fix-missing --force-yes install language-pack-en &&\
    update-locale LANG=en_US.UTF-8 LANGUAGE=en_US.UTF-8 LC_ALL=en_US.UTF-8 &&\
    dpkg-reconfigure locales &&\
    apt-get -y install curl sudo &&\
    curl -s https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - &&\
    sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" >> /etc/apt/sources.list.d/postgresql.list' &&\
    apt-get update &&\
    apt-get install -y -q \
      less \
      postgresql-9.4 \
      postgresql-contrib-9.4 \
      postgresql-9.4-pgextwlist \
      postgresql-9.4-plv8 \
      postgresql-9.4-postgis \
      postgresql-9.4-pgrouting &&\
    apt-get clean &&\
    apt-get autoremove -y &&\
    rm /etc/apt/apt.conf.d/10_docker_hostproxy || true &&\
    echo "\set HISTFILE /dev/null" > /root/.psqlrc

ADD bin/flynn-postgres /bin/flynn-postgres
ADD bin/flynn-postgres-api /bin/flynn-postgres-api
ADD start.sh /bin/start-flynn-postgres

ENTRYPOINT ["/bin/start-flynn-postgres"]
