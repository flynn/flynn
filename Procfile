etcd: bin/etcd
discoverd: bin/discoverd
gitreceive: bin/gitreceived -p 2022 id_rsa bin/flynn-receive
shelf: sleep 1 && bin/sdutil exec -h 10.0.2.15 shelf:8888 bin/shelf -p 8888 /vagrant/storage
strowger: sleep 1 && bin/strowger
host: sleep 2 && bin/flynn-host -external 10.0.2.15
controller: sleep 3 && bin/flynn-controller
