
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