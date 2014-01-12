build:
ifdef LOCAL
	make build/flynn-controller
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/flynn-controller | tar -xC build
endif

build/flynn-controller:
	godep go build -o build/flynn-controller

container: build
	docker build -t flynn/controller .

clean:
	rm -rf build

.PHONY: build
