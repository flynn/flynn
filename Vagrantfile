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

  config.vm.synced_folder ".", "/home/vagrant/go/src/github.com/flynn/flynn", create: true, group: "vagrant", owner: "vagrant"

  if Vagrant.has_plugin?("vagrant-vbguest")
    # vagrant-vbguest can cause the VM to not start: https://github.com/flynn/flynn/issues/2874
    config.vbguest.auto_update = false
  end

  # Override locale settings. Avoids host locale settings being sent via SSH
  ENV['LC_ALL']="en_US.UTF-8"
  ENV['LANG']="en_US.UTF-8"
  ENV['LANGUAGE']="en_US.UTF-8"

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
    aws.block_device_mapping = [{ 'DeviceName' => '/dev/sda1', 'Ebs.DeleteOnTermination' => true }]

    override.vm.box_version = nil
    override.vm.box_url = "https://github.com/mitchellh/vagrant-aws/raw/master/dummy.box"
    override.ssh.username = "ubuntu"
    override.ssh.private_key_path = ENV["FLYNN_AWS_KEY"]
  end

  config.vm.provision "shell", privileged: false, inline: <<-SCRIPT
    grep '^export GOPATH' ~/.bashrc || echo export GOPATH=~/go >> ~/.bashrc
    grep '^export DISCOVERD' ~/.bashrc || echo export DISCOVERD="192.0.2.200:1111" >> ~/.bashrc
    grep '^export GOROOT' ~/.bashrc || echo export GOROOT=~/go/src/github.com/flynn/flynn/util/_toolchain/go >> ~/.bashrc
    grep '^export PATH' ~/.bashrc || echo export PATH=~/go/bin:~/go/src/github.com/flynn/flynn/util/_toolchain/go/bin:~/go/src/github.com/flynn/flynn/discoverd/bin:~/go/src/github.com/flynn/flynn/cli/bin:~/go/src/github.com/flynn/flynn/host/bin:~/go/src/github.com/flynn/flynn/script:\\\$PATH: >> ~/.bashrc

    # For script unit tests
    tmpdir=$(mktemp --directory)
    trap "rm -rf ${tmpdir}" EXIT
    git clone https://github.com/sstephenson/bats.git "${tmpdir}/bats"
    sudo "${tmpdir}/bats/install.sh" /usr/local
    sudo curl -sLo "/usr/local/bin/jq" "http://stedolan.github.io/jq/download/linux64/jq"
    sudo chmod +x "/usr/local/bin/jq"

    # Database dependencies - postgres, mariadb + percona xtrabackup, mongodb, redis
    sudo apt-key adv --recv-keys --keyserver hkp://keyserver.ubuntu.com:80 ACCC4CF8 1BB943DB CD2EFD2A EA312927 C7917B12
    sudo sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt/ trusty-pgdg main" > /etc/apt/sources.list.d/postgresql.list'
    sudo sh -c 'echo "deb http://mirrors.syringanetworks.net/mariadb/repo/10.1/ubuntu trusty main" > /etc/apt/sources.list.d/mariadb.list'
    sudo sh -c 'echo "deb http://repo.percona.com/apt trusty main" > /etc/apt/sources.list.d/percona.list'
    sudo sh -c 'echo "deb http://repo.mongodb.org/apt/ubuntu trusty/mongodb-org/3.2 multiverse" > /etc/apt/sources.list.d/mongodb.list'
    sudo sh -c 'echo "deb http://ppa.launchpad.net/chris-lea/redis-server/ubuntu trusty main" > /etc/apt/sources.list.d/redis.list'
    sudo apt-get update
    sudo sh -c 'DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql-9.5 postgresql-contrib-9.5 mariadb-server percona-xtrabackup mongodb-org redis-server'

    # Setup postgres for controller unit tests
    sudo -u postgres createuser --superuser vagrant || true
    grep '^export PGHOST' ~/.bashrc || echo export PGHOST=/var/run/postgresql >> ~/.bashrc

    # For integration tests.
    #
    # Override these in script/custom-vagrant if you use git to make
    # real commits in the VM.
    git config --global user.email "flynn.dev@example.com"
    git config --global user.name "Flynn Dev"

    grep ^cd ~/.bashrc || echo cd ~/go/src/github.com/flynn/flynn >> ~/.bashrc
    sudo chown -R vagrant:vagrant ~/go

    # enable docker
    sudo rm -f /etc/init/docker.override
    sudo start docker || true
  SCRIPT

  if File.exists?("script/custom-vagrant")
    config.vm.provision "shell", path: "script/custom-vagrant"
  end
end
