etcd: etcd -d /tmp -v
discoverd: ./go-discover/discoverd/discoverd
gitreceive: gitreceived -p 2022 id_rsa ./flynn-receive
shelf: sleep 1 && sdutil exec -h $(curl -s icanhazip.com) shelf:8888 shelf -p 8888 /var/lib/demo/storage
sampi: sleep 1 && ./sampi/sampid/sampid
lorne: sleep 2 && ./lorne/lorne