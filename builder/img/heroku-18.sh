#!/bin/bash

# Derived from https://github.com/heroku/stack-images
# Copyright (c) 2015, Heroku, Inc.

set -e
export LC_ALL=C
export DEBIAN_FRONTEND=noninteractive

cat >/etc/apt/sources.list <<EOF
deb http://archive.ubuntu.com/ubuntu/ bionic main universe
deb http://archive.ubuntu.com/ubuntu/ bionic-security main universe
deb http://archive.ubuntu.com/ubuntu/ bionic-updates main universe
deb http://apt.postgresql.org/pub/repos/apt/ bionic-pgdg main
EOF

apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8

apt-get update
apt-get upgrade -y
apt-get install -y --no-install-recommends \
    apt-transport-https \
    apt-utils \
    bind9-host \
    bzip2 \
    coreutils \
    curl \
    dnsutils \
    ed \
    file \
    fontconfig \
    gcc \
    geoip-database \
    ghostscript \
    git \
    gsfonts \
    imagemagick \
    iproute2 \
    iputils-tracepath \
    language-pack-en \
    less \
    libargon2-0 \
    libcairo2 \
    libcroco3 \
    libcurl4 \
    libdatrie1 \
    libev4 \
    libevent-2.1-6 \
    libevent-core-2.1-6 \
    libevent-extra-2.1-6 \
    libevent-openssl-2.1-6 \
    libevent-pthreads-2.1-6 \
    libexif12 \
    libgd3 \
    libgdk-pixbuf2.0-0 \
    libgdk-pixbuf2.0-common \
    libgnutls-openssl27 \
    libgnutlsxx28 \
    libgraphite2-3 \
    libgs9 \
    libharfbuzz0b \
    libmagickcore-6.q16-3-extra \
    libmcrypt4 \
    libmemcached11 \
    libmysqlclient20 \
    libpango-1.0-0 \
    libpangocairo-1.0-0 \
    libpangoft2-1.0-0 \
    libpixman-1-0 \
    librabbitmq4 \
    librsvg2-2 \
    librsvg2-common \
    libsasl2-modules \
    libseccomp2 \
    libsodium23 \
    libthai-data \
    libthai0 \
    libuv1 \
    libxcb-render0 \
    libxcb-shm0 \
    libxrender1 \
    libxslt1.1 \
    libzip4 \
    locales \
    lsb-release \
    make \
    netcat-openbsd \
    openssh-client \
    openssh-server \
    patch \
    postgresql-client-11 \
    python \
    rename \
    rsync \
    ruby \
    shared-mime-info \
    socat \
    stunnel \
    syslinux \
    tar \
    telnet \
    tzdata \
    unzip \
    wget \
    xz-utils \
    zip \
    pigz \
    daemontools \

cat > /etc/ImageMagick-6/policy.xml <<'IMAGEMAGICK_POLICY'
<policymap>
  <policy domain="resource" name="memory" value="256MiB"/>
  <policy domain="resource" name="map" value="512MiB"/>
  <policy domain="resource" name="width" value="16KP"/>
  <policy domain="resource" name="height" value="16KP"/>
  <policy domain="resource" name="area" value="128MB"/>
  <policy domain="resource" name="disk" value="1GiB"/>
  <policy domain="delegate" rights="none" pattern="URL" />
  <policy domain="delegate" rights="none" pattern="HTTPS" />
  <policy domain="delegate" rights="none" pattern="HTTP" />
  <policy domain="path" rights="none" pattern="@*"/>
  <policy domain="cache" name="shared-secret" value="passphrase" stealth="true"/>
</policymap>
IMAGEMAGICK_POLICY

# install the JDK for certificates, then remove it
apt-get install -y --no-install-recommends ca-certificates-java openjdk-8-jre-headless
apt-get remove -y ca-certificates-java
apt-get -y --purge autoremove
apt-get purge -y openjdk-8-jre-headless
test "$(file -b /etc/ssl/certs/java/cacerts)" = "Java KeyStore"

cd /
rm -rf /root/*
rm -rf /tmp/*
rm -rf /var/cache/apt/archives/*.deb
rm -rf /var/lib/apt/lists/*
