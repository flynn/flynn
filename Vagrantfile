# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"
  config.vm.box_url = "https://github.com/flynn/flynn-demo/releases/download/v0.5.0/flynn-base_virtualbox.box"
  config.vm.box_download_checksum = "ae3c0701ceb588f274b35ee6960d83225f0bb470cb18543dd3e01f6b36b4c934"
  config.vm.box_download_checksum_type = "sha256"

  config.vm.network "forwarded_port", guest: 80, host: 8080
  config.vm.network "forwarded_port", guest: 443, host: 8081
  config.vm.network "forwarded_port", guest: 2222, host: 2201

  config.vm.synced_folder ".", "/vagrant", disabled: true

  config.vm.provision "shell", inline: <<SCRIPT
    # Fix for https://github.com/flynn/flynn/issues/13
    echo 3600 > /proc/sys/net/netfilter/nf_conntrack_tcp_timeout_close_wait

    IP_ADDR=$(/sbin/ifconfig eth0 | grep 'inet addr:' | cut -d: -f2 | awk '{ print $1}')

    echo "Configuring flynn with internal ip: ${IP_ADDR}"

    docker pull flynn/host
    docker pull flynn/discoverd
    docker pull flynn/etcd

    docker run -d -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113 flynn/host -external ${IP_ADDR} -force

    docker pull flynn/postgres
    docker pull flynn/controller
    docker pull flynn/gitreceive
    docker pull flynn/strowger
    docker pull flynn/shelf
    docker pull flynn/slugrunner
    docker pull flynn/slugbuilder
    docker pull flynn/bootstrap

    docker run -e=DISCOVERD=${IP_ADDR}:1111 flynn/bootstrap
SCRIPT
end
