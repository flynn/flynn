#!/bin/bash

# Derived from https://github.com/heroku/stack-images/blob/master/bin/cedar-14.sh

echo 'deb http://archive.ubuntu.com/ubuntu trusty main restricted' >/etc/apt/sources.list
echo 'deb http://archive.ubuntu.com/ubuntu trusty-updates main restricted' >>/etc/apt/sources.list
echo 'deb http://archive.ubuntu.com/ubuntu trusty universe' >>/etc/apt/sources.list
echo 'deb http://archive.ubuntu.com/ubuntu trusty-updates universe' >>/etc/apt/sources.list
echo 'deb http://archive.ubuntu.com/ubuntu trusty-security main restricted' >>/etc/apt/sources.list
echo 'deb http://archive.ubuntu.com/ubuntu trusty-security universe' >>/etc/apt/sources.list

apt-get update
apt-get dist-upgrade -y

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
  stunnel \
  syslinux \
  tar \
  telnet \
  zip \
  zlib1g-dev \
  pigz

# Install locales
apt-cache search language-pack \
  | cut -d ' ' -f 1 \
  | grep -v '^language\-pack\-\(gnome\|kde\)\-' \
  | grep -v '\-base$' \
  | xargs apt-get install -y --force-yes --no-install-recommends

# Workaround for CVE-2016–3714 until new ImageMagick packages come out.
echo '<policymap> <policy domain="coder" rights="none" pattern="EPHEMERAL" /> <policy domain="coder" rights="none" pattern="URL" /> <policy domain="coder" rights="none" pattern="HTTPS" /> <policy domain="coder" rights="none" pattern="MVG" /> <policy domain="coder" rights="none" pattern="MSL" /> <policy domain="coder" rights="none" pattern="TEXT" /> <policy domain="coder" rights="none" pattern="SHOW" /> <policy domain="coder" rights="none" pattern="WIN" /> <policy domain="coder" rights="none" pattern="PLT" /> </policymap>' > /etc/ImageMagick/policy.xml

rm -rf /var/cache/apt/archives/*.deb
rm -rf /root/*
rm -rf /tmp/*
rm /etc/ssh/ssh_host_*
