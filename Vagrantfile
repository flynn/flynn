# -*- mode: ruby -*-
# vi: set ft=ruby :

# Fail if Vagrant version is too old
begin
  Vagrant.require_version ">= 1.6.0"
rescue NoMethodError
  $stderr.puts "This Vagrantfile requires Vagrant version >= 1.6.0"
  exit 1
end

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"
  config.vm.box_url = "https://dl.flynn.io/vagrant/flynn-base.json"
  config.vm.box_version = "> 0"

  # RFC 5737 TEST-NET-1 used to avoid DNS rebind protection
  config.vm.network "private_network", ip: "192.0.2.100"

  config.vm.provider "virtualbox" do |v|
    v.memory = ENV["VAGRANT_MEMORY"] || 1024
    v.cpus = ENV["VAGRANT_CPUS"] || 2
  end

  config.vm.provision "shell", privileged: false, inline: <<SCRIPT
    grep '^export GOPATH' ~/.bashrc || echo export GOPATH=~/go >> ~/.bashrc
    grep '^export PATH' ~/.bashrc || echo export PATH=\\\$PATH:~/go/bin:/vagrant/script >> ~/.bashrc
    GOPATH=~/go go get github.com/tools/godep

    # For controller tests
    sudo apt-get update
    sudo apt-get install -y postgresql postgresql-contrib
    sudo -u postgres createuser --superuser vagrant
    grep '^export PGHOST' ~/.bashrc || echo export PGHOST=/var/run/postgresql >> ~/.bashrc

    mkdir -p ~/go/src/github.com/flynn
    ln -s /vagrant ~/go/src/github.com/flynn/flynn
    grep ^cd ~/.bashrc || echo cd ~/go/src/github.com/flynn/flynn >> ~/.bashrc
SCRIPT

  if File.exists?("script/custom-vagrant")
    config.vm.provision "shell", path: "script/custom-vagrant"
  end
end
