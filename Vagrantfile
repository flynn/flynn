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

# Return a nested hash of amis, indexed by region and root_device_type
def get_amis
  require "open-uri"
  require "json"

  images_uri = "https://dl.flynn.io/ec2/images.json"
  json = JSON.parse(open(images_uri).read())
  latest = json["versions"][0]["images"]

  Hash[ latest.map { |ami| [ ami["region"], Hash[ ami["root_device_type"], ami["id"] ] ] } ]
end

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "flynn-base"
  config.vm.box_url = "https://dl.flynn.io/vagrant/flynn-base.json"
  config.vm.box_version = "> 0"

  # VAGRANT_MEMORY          - instance memory, in MB
  # VAGRANT_CPUS            - instance virtual CPUs
  config.vm.provider "virtualbox" do |v, override|
    v.memory = ENV["VAGRANT_MEMORY"] || 4096
    v.cpus = ENV["VAGRANT_CPUS"] || 4

    # RFC 5737 TEST-NET-1 used to avoid DNS rebind protection
    override.vm.network "private_network", ip: "192.0.2.100"
  end

  config.vm.provider "vmware_fusion" do |v, override|
    v.vmx["memsize"] = ENV["VAGRANT_MEMORY"] || 4096
    v.vmx["numvcpus"] = ENV["VAGRANT_CPUS"] || 4

    # RFC 5737 TEST-NET-1 used to avoid DNS rebind protection
    override.vm.network "private_network", ip: "192.0.2.100"
  end

  # AWS_ACCESS_KEY_ID       - AWS IAM public ID
  # AWS_SECRET_ACCESS_KEY   - AWS IAM secret key
  # AWS_SESSION_TOKEN       - AWS IAM role session token
  # AWS_DEFAULT_REGION      - AWS EC2 region to launch instance in
  # FLYNN_AWS_KEY       - SSH key (.pem file) used to connect to the instance
  # FLYNN_AWS_KEYNAME   - AWS EC2 key pair name
  # FLYNN_AWS_SUBNET    - AWS VPC subnet to launch instance in
  # FLYNN_AWS_IAM       - AWS IAM role (by name) to give instance permissions
  # FLYNN_AWS_IAM_ARN   - AWS IAM role (by ARN) to give instance permissions
  # FLYNN_AWS_TAGS      - AWS EC2 tags to apply to instance (comma separated key-value pairs)
  config.vm.provider "aws" do |aws, override|

    aws.access_key_id = ENV["AWS_ACCESS_KEY_ID"]
    aws.secret_access_key = ENV["AWS_SECRET_ACCESS_KEY"]
    aws.session_token = ENV["AWS_SESSION_TOKEN"]
    aws.keypair_name = ENV["FLYNN_AWS_KEYNAME"]

    get_amis().each_pair do |region,amis|
      aws.region_config region, :ami => amis["ebs"]
    end

    aws.region = ( ENV["AWS_DEFAULT_REGION"] || "us-east-1" )

    aws.instance_type = "m3.large"
    aws.subnet_id = ENV["FLYNN_AWS_SUBNET"]
    aws.iam_instance_profile_name = ENV["FLYNN_AWS_IAM"]
    aws.iam_instance_profile_arn = ENV["FLYNN_AWS_IAM_ARN"]
    added_tags = Hash[ String( ENV["FLYNN_AWS_TAGS"] ).split(",").map {|kv| kv.split("=")} ]
    aws.tags = { "Name" => 'flynn-vagrant' }.merge(added_tags)

    override.vm.box_version = nil
    override.vm.box_url = "https://github.com/mitchellh/vagrant-aws/raw/master/dummy.box"
    override.ssh.username = "ubuntu"
    override.ssh.private_key_path = ENV["FLYNN_AWS_KEY"]
  end

  config.vm.provision "shell", privileged: false, inline: <<-SCRIPT
    grep '^export GOPATH' ~/.bashrc || echo export GOPATH=~/go >> ~/.bashrc
    grep '^export PATH' ~/.bashrc || echo export PATH=\\\$PATH:~/go/bin:~/go/src/github.com/flynn/flynn/appliance/etcd/bin:~/go/src/github.com/flynn/flynn/discoverd/bin:/vagrant/script >> ~/.bashrc
    GOPATH=~/go go get github.com/tools/godep

    # For script unit tests
    tmpdir=$(mktemp --directory)
    trap "rm -rf ${tmpdir}" EXIT
    git clone https://github.com/sstephenson/bats.git "${tmpdir}/bats"
    sudo "${tmpdir}/bats/install.sh" /usr/local
    sudo curl -sLo "/usr/local/bin/jq" "http://stedolan.github.io/jq/download/linux64/jq"
    sudo chmod +x "/usr/local/bin/jq"

    # For controller tests
    sudo apt-get update
    sudo apt-get install -y postgresql postgresql-contrib
    sudo -u postgres createuser --superuser vagrant
    grep '^export PGHOST' ~/.bashrc || echo export PGHOST=/var/run/postgresql >> ~/.bashrc

    # For integration tests.
    #
    # Override these in script/custom-vagrant if you use git to make
    # real commits in the VM.
    git config --global user.email "flynn.dev@example.com"
    git config --global user.name "Flynn Dev"

    mkdir -p ~/go/src/github.com/flynn
    ln -s /vagrant ~/go/src/github.com/flynn/flynn
    grep ^cd ~/.bashrc || echo cd ~/go/src/github.com/flynn/flynn >> ~/.bashrc
  SCRIPT

  if File.exists?("script/custom-vagrant")
    config.vm.provision "shell", path: "script/custom-vagrant"
  end
end
