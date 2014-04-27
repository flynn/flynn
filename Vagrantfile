# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"
  config.vm.box_url = "https://github.com/flynn/flynn-demo/releases/download/v0.1.0/flynn-base_virtualbox.box"
  config.vm.box_download_checksum = "18584b5c733bb11aa8b312b5365e6d85fe24352d56ce874b874fe51724b21c28"
  config.vm.box_download_checksum_type = "sha256"

  config.vm.network "forwarded_port", guest: 80, host: 8080
  config.vm.network "forwarded_port", guest: 443, host: 8081
  config.vm.network "forwarded_port", guest: 2222, host: 2201
  config.ssh.port = 2201
  config.ssh.guest_port = 2222

  config.vm.synced_folder ".", "/vagrant", disabled: true

  config.vm.provision "shell", inline: <<SCRIPT
    apt-get update
    sudo apt-get install -y lxc-docker

    # Fix for https://github.com/flynn/flynn/issues/13
    echo 3600 > /proc/sys/net/netfilter/nf_conntrack_tcp_timeout_close_wait

    docker run -d -v=/var/run/docker.sock:/var/run/docker.sock -p=1113:1113 flynn/host -external 10.0.2.15 -force
    docker pull flynn/postgres
    docker pull flynn/controller
    docker pull flynn/gitreceive
    docker pull flynn/strowger
    docker pull flynn/shelf
    docker pull flynn/slugrunner
    docker pull flynn/slugbuilder

    docker run -e=DISCOVERD=10.0.2.15:1111 flynn/bootstrap
SCRIPT
end
