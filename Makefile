build/container: build/shelf
	docker build -t flynn/shelf .
	touch build/container

build/shelf:
	godep go build -o build/shelf

.PHONY: clean
clean:
	rm -rf build tmp
