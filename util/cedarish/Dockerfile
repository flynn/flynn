FROM ubuntu-debootstrap:14.04

# Derived from https://github.com/heroku/stack-images/blob/master/Dockerfile

RUN echo 'deb http://archive.ubuntu.com/ubuntu trusty main' >/etc/apt/sources.list &&\
    echo 'deb http://archive.ubuntu.com/ubuntu trusty-security main' >>/etc/apt/sources.list &&\
    echo 'deb http://archive.ubuntu.com/ubuntu trusty-updates main' >>/etc/apt/sources.list &&\
    echo 'deb http://archive.ubuntu.com/ubuntu trusty universe' >>/etc/apt/sources.list &&\

    apt-get update &&\
    apt-get dist-upgrade -y &&\

    apt-get install -y --force-yes \
      autoconf \
      bind9-host \
      bison \
      build-essential \
      coreutils \
      curl \
      daemontools \
      dnsutils \
      ed \
      git \
      imagemagick \
      iputils-tracepath \
      language-pack-en \
      libbz2-dev \
      libcurl4-openssl-dev \
      libevent-dev \
      libglib2.0-dev \
      libjpeg-dev \
      libmagickwand-dev \
      libmysqlclient-dev \
      libncurses5-dev \
      libpq-dev \
      libpq5 \
      libreadline6-dev \
      libssl-dev \
      libxml2-dev \
      libxslt-dev \
      netcat-openbsd \
      openjdk-7-jdk \
      openjdk-7-jre-headless \
      openssh-client \
      openssh-server \
      postgresql-server-dev-9.3 \
      python \
      python-dev \
      ruby \
      ruby-dev \
      socat \
      syslinux \
      tar \
      telnet \
      zip \
      zlib1g-dev \
      pigz &&\

    rm /etc/ssh/ssh_host_*
