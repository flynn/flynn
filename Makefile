
# Setup

setup: key 
	mkdir -p /var/lib/demo/storage
	mkdir -p /var/lib/demo/apps

key:
	ssh-keygen -t rsa -N "" -f id_rsa

# Projects

sampi:
	git clone https://github.com/flynn/sampi
	cd sampi/sampid && go get || true && go build

lorne:
	git clone https://github.com/flynn/lorne
	cd lorne && go get || true && go build

flynn-receive:
	go build -o flynn-receive

slugbuilder:
	docker pull flynn/slugbuilder

slugrunner:
	docker pull flynn/slugrunner

gitreceive:
	git clone https://github.com/flynn/gitreceive-next.git
	cd gitreceive-next && make install

discoverd:
	git clone https://github.com/flynn/go-discover.git
	cd go-discover/discoverd && make install

sdutil:
	wget http://progrium-sdutil.s3.amazonaws.com/sdutil_0.1.0_amd64.deb
	dpkg -i sdutil_0.1.0_amd64.deb

shelf:
	git clone https://github.com/flynn/shelf.git
	cd shelf && make install

## Vendor

packages: docker go etcd
	apt-get install -y ruby1.9.1 rubygems mercurial
	gem install foreman --no-rdoc --no-ri

etcd:
	wget https://github.com/coreos/etcd/releases/download/v0.1.2/etcd-v0.1.2-Linux-x86_64.tar.gz
	tar -zxvf etcd-v0.1.2-Linux-x86_64.tar.gz
	cp etcd-v0.1.2-Linux-x86_64/etcd /usr/local/bin

go:
	wget http://j.mp/godeb
	tar -zxvf ./godeb
	./godeb install 1.1.2

docker: aufs
	egrep -i "^docker" /etc/group || groupadd docker
	curl https://get.docker.io/gpg | apt-key add -
	echo deb http://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list
	apt-get update
	apt-get install -y lxc-docker 
	sleep 2 # give docker a moment i guess

aufs:
	lsmod | grep aufs || modprobe aufs || apt-get install -y linux-image-extra-`uname -r`
