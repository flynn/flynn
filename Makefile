COMMIT := 215ff2839dc5022768f5561b088e0b61ee759c40

etcd/bin/container: etcd/bin/etcd Dockerfile
	docker build -t flynn/etcd .
	touch etcd/bin/container

etcd/bin/etcd: etcd/build
	cd etcd && ./build

etcd/build: etcd.tar.gz
	rm -rf etcd-$(COMMIT)/ etcd/
	tar xzf etcd.tar.gz
	mv etcd-$(COMMIT) etcd
	touch etcd/build

etcd.tar.gz:
	curl -Lo etcd.tar.gz https://github.com/coreos/etcd/archive/$(COMMIT).tar.gz

.PHONY: clean
clean:
	rm -rf *.tar.gz etcd/ etcd-$(COMMIT)/
