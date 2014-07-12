# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"
  config.vm.box_url = "https://github.com/flynn/flynn-demo/releases/download/v0.4.0/flynn-base_virtualbox.box"
  config.vm.box_download_checksum = "24c050afd6226d59fcdfc90e6885746c4438e6cafea8518c71acd47d618b9ce5"
  config.vm.box_download_checksum_type = "sha256"

  config.vm.network "forwarded_port", guest: 80, host: 8080
  config.vm.network "forwarded_port", guest: 443, host: 8081
  config.vm.network "forwarded_port", guest: 2222, host: 2201

  config.vm.provision "shell", inline: <<SCRIPT
    apt-get update
    apt-get install -y software-properties-common libdevmapper-dev btrfs-tools libvirt-dev
    apt-add-repository 'deb http://ppa.launchpad.net/anatol/tup/ubuntu precise main'
    apt-key adv --keyserver keyserver.ubuntu.com --recv E601AAF9486D3664
    apt-get update
    apt-get install -y tup

    # Fix for https://github.com/flynn/flynn/issues/13
    echo 3600 > /proc/sys/net/netfilter/nf_conntrack_tcp_timeout_close_wait
SCRIPT

  config.vm.provision "shell", privileged: false, inline: <<SCRIPT
    grep '^export GOPATH' ~/.bashrc || echo export GOPATH=~/go >> ~/.bashrc
    grep '^export PATH' ~/.bashrc || echo export PATH=\$PATH:~/go/bin:/vagrant/script >> ~/.bashrc
    GOPATH=~/go go get github.com/tools/godep

    mkdir -p ~/go/src/github.com/flynn
    ln -s /vagrant ~/go/src/github.com/flynn/flynn
    grep ^cd ~/.bashrc || echo cd ~/go/src/github.com/flynn/flynn >> ~/.bashrc
SCRIPT
end
