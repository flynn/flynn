build:
ifdef LOCAL
	make build/flynn-host
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/flynn-host | tar -xC build
endif

build/flynn-host:
	godep go build -o build/flynn-host

container: build
	docker build -t flynn/host .

clean:
	rm -rf build

.PHONY: build
