VERSION = 0.1.0

build: 
ifdef LOCAL
	make build/shelf
else
	mkdir -p build && tar -cf - . | docker run -i -a stdin -a stdout progrium/makebuilder | tar -xC build
endif

build/shelf:
	go build -o build/shelf

container: build
	docker build -t flynn/shelf .

install: build
	cp shelf /usr/local/bin

clean:
	rm -rf build