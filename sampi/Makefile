
build:
	mkdir -p build
	#godep go build -o build/sampid ./sampid
	cd sampid && go get || true
	go build -o build/sampid ./sampid

container: build
	docker build -t flynn/sampi .

clean:
	rm -rf build

.PHONY: build
