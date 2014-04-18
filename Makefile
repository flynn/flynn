build/container: build/flynn-receive Dockerfile taffy.sh
	docker build -t flynn/taffy .
	touch build/container

build/flynn-receive:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/flynn-receive
	GOPATH=. cd tmp/flynn-receive && godep go build -o ../../build/flynn-receive
	rm -rf tmp

.PHONY: clean
clean:
	rm -rf build tmp

