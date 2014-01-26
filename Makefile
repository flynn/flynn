build:
ifdef DOCKER
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/flynn-host | tar -xC build
else
	mkdir -p build
	godep go build -o build/flynn-host
endif

container: build
	docker build -t flynn/host .

clean:
	rm -rf build

.PHONY: build
