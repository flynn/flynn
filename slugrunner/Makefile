build/container: build/sdutil Dockerfile runner/*
	docker build -t flynn/slugrunner .
	touch build/container

build/sdutil:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/sdutil.git
	GOPATH=. cd tmp/sdutil && godep go build -o ../../build/sdutil
	rm -rf tmp

.PHONY: clean
clean:
	rm -rf tmp build
