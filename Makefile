
setup: key 
	mkdir -p /var/lib/demo/storage
	mkdir -p /var/lib/demo/apps

key:
	ssh-keygen -t rsa -N "" -f id_rsa

foreman:
	gem install foreman

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

etcd:
	wget https://github.com/coreos/etcd/releases/download/v0.2.0-rc0/etcd-v0.2.0-rc0-Linux-x86_64.tar.gz
	tar -zxvf etcd-v0.2.0-rc0-Linux-x86_64.tar.gz
	cp etcd-v0.2.0-rc0-Linux-x86_64/etcd /usr/local/bin

shelf:
	git clone https://github.com/flynn/shelf.git
	cd shelf && make install

docker: aufs
	egrep -i "^docker" /etc/group || groupadd docker
	curl https://get.docker.io/gpg | apt-key add -
	echo deb http://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list
	apt-get update
	apt-get install -y lxc-docker 
	sleep 2 # give docker a moment i guess

aufs:
	lsmod | grep aufs || modprobe aufs || apt-get install -y linux-image-extra-`uname -r`