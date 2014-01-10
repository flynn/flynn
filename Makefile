build:
ifdef LOCAL
	make build/lorne
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/lorne | tar -xC build
endif

build/lorne:
	godep go build -o build/lorne

container: build
	docker build -t flynn/lorne .

clean:
	rm -rf build

.PHONY: build
