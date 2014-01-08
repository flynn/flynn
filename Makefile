
build:
	mkdir -p build
	godep go build -o build/discoverd

container: build
	docker build -t flynn/discoverd .

clean:
	rm -rf build

.PHONY: build