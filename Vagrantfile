# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"

  config.vm.network "forwarded_port", guest: 80, host: 8080
  config.vm.network "forwarded_port", guest: 443, host: 8081
  config.vm.network "forwarded_port", guest: 2222, host: 2201
  config.ssh.port = 2201
  config.ssh.guest_port = 2222

  config.vm.synced_folder ".", "/vagrant", disabled: true

  config.vm.provision "shell", inline: <<SCRIPT
    apt-get update
    sudo apt-get install -y lxc-docker

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
