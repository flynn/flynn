build/container: build/flynn-receive build/gitreceived Dockerfile start.sh
	docker build -t flynn/gitreceive .
	touch build/container

build/flynn-receive: *.go
	godep go build -o build/flynn-receive

build/gitreceived:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/gitreceived
	GOPATH=. cd tmp/gitreceived && godep go build -o ../../build/gitreceived
	rm -rf tmp

.PHONY: clean
clean:
	rm -rf build tmp
