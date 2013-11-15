# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
#
# We use Phusion's[Docker-friendly Vagrant box (http://blog.phusion.nl/2013/11/08/docker-friendly-vagrant-boxes/)
# as Vagrant base box
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "phusion_precise64"
  config.vm.box_url = "https://oss-binaries.phusionpassenger.com/vagrant/boxes/ubuntu-12.04.3-amd64-vbox.box"
  config.vm.synced_folder "./", "/vagrant"

  config.vm.provision :shell, inline: <<-SCRIPT
    sudo -u vagrant sh -c "echo 'export GOPATH=/vagrant' >> /home/vagrant/.bashrc"
    sudo -u vagrant sh -c "echo 'PATH=/vagrant/bin:$PATH' >> /home/vagrant/.bashrc"
    sudo -u vagrant sh -c "echo 'cd /vagrant' >> /home/vagrant/.bashrc"
  SCRIPT
end
