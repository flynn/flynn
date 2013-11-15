# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "phusion_ubuntu_12_04"
  config.vm.box_url = "https://oss-binaries.phusionpassenger.com/vagrant/boxes/ubuntu-12.04.3-amd64-vbox.box"
  config.vm.synced_folder "./", "/vagrant"

  config.vm.provision :shell, inline: <<-SCRIPT
    sudo -u vagrant sh -c "echo 'export GOPATH=/vagrant' >> /home/vagrant/.bashrc"
    sudo -u vagrant sh -c "echo 'PATH=/vagrant/bin:$PATH' >> /home/vagrant/.bashrc"
    sudo -u vagrant sh -c "echo 'cd /vagrant' >> /home/vagrant/.bashrc"

    # install latest kernel
    apt-get update
    apt-get install -y make
  SCRIPT
end
