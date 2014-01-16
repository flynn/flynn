build:
ifdef DOCKER
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/lorne | tar -xC build
else
	mkdir -p build
	godep go build -o build/lorne
endif

container: build
	docker build -t flynn/lorne .

clean:
	rm -rf build

.PHONY: build
