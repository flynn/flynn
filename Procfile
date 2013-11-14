etcd: bin/etcd -d /tmp
discoverd: bin/discoverd
gitreceive: bin/gitreceived -p 2022 id_rsa bin/flynn-receive
shelf: sleep 1 && bin/sdutil exec -h 10.0.2.15 shelf:8888 bin/shelf -p 8888 /vagrant/storage
sampi: sleep 1 && bin/sampid
strowger: sleep 1 && bin/strowger
lorne: sleep 2 && bin/lorne -external 10.0.2.15
api: sleep 2 && bin/flynn-api
