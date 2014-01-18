build:
ifdef LOCAL
	make build/sampid
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/sampi | tar -xC build
endif

build/sampid:
	godep go build -o build/sampid ./sampid

container: build
	docker build -t flynn/sampi .

clean:
	rm -rf build

.PHONY: build
