build/container: build/flynn-receive build/flynn-key-check build/gitreceived build/sdutil Dockerfile start.sh
	docker build -t flynn/gitreceive .
	touch build/container

build/flynn-receive: *.go
	godep go build -o build/flynn-receive

build/flynn-key-check: key-check/*.go
	godep go build -o build/flynn-key-check ./key-check

build/gitreceived:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/gitreceived
	GOPATH=. cd tmp/gitreceived && godep go build -o ../../build/gitreceived
	rm -rf tmp

build/sdutil:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/sdutil
	GOPATH=. cd tmp/sdutil && godep go build -o ../../build/sdutil
	rm -rf tmp

.PHONY: clean
clean:
	rm -rf build tmp
