
build:
	mkdir -p build
	godep go build -o build/lorne

container: build
	docker build -t flynn/lorne .

clean:
	rm -rf build

.PHONY: build
