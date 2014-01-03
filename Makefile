build:
ifdef LOCAL
	make build/strowger
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout -e=GOPATH=/tmp/go titanous/makebuilder makebuild go/src/github.com/flynn/strowger | tar -xC build
endif

build/strowger:
	godep go build -o build/strowger

container: build
	docker build -t flynn/strowger .

clean:
	rm -rf build

.PHONY: build
