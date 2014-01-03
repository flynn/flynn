# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-precise64"
  config.vm.box_url = "https://s3.amazonaws.com/flynn/flynn-virtualbox-ubuntu_12.04.3-amd64.box"
  config.vm.box_download_checksum = "d222d515e83e0d8a547c55f9e5cbaec703fd414f0d761193d9fee1c6066504cf"
  config.vm.box_download_checksum_type = "sha256"
  config.vm.synced_folder "./", "/vagrant"

  config.vm.provision :docker
end
