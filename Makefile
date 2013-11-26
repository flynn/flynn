VERSION = 0.1.0

build:
ifdef LOCAL
	make build/shelf
	make build/sdutil
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout progrium/makebuilder | tar -xC build
endif

build/shelf:
	go build -o build/shelf

build/sdutil:
	mkdir -p tmp
	cd tmp && git clone https://github.com/flynn/sdutil.git
	GOPATH=. cd tmp/sdutil && go get || true && go build -o ../../build/sdutil
	rm -rf tmp

container: build
	docker build -t flynn/shelf .

install: build
	cp shelf /usr/local/bin

clean:
	rm -rf build tmp shelf