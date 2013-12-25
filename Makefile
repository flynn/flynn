build:
ifdef LOCAL
	make build/discoverd
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go progrium/makebuilder makebuild go/src/github.com/flynn/go-discover | tar -xC build
endif

build/discoverd:
	godep go build -o build/discoverd ./discoverd

container: build
	docker build -t flynn/discoverd .

clean:
	rm -rf build tmp discoverd
