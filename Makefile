build/container: build/shelf build/sdutil
	docker build -t flynn/shelf .
	touch build/container

build/shelf:
	godep go build -o build/shelf

build/sdutil:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/sdutil.git
	GOPATH=. cd tmp/sdutil && godep go build -o ../../build/sdutil
	rm -rf tmp

.PHONY: clean
clean:
	rm -rf build tmp
