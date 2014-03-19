build: build/sdutil
	docker build -t flynn/slugrunner .

build/sdutil:
	mkdir -p tmp build
	cd tmp && git clone https://github.com/flynn/sdutil.git
	GOPATH=. cd tmp/sdutil && go get || true && go build -o ../../build/sdutil
	rm -rf tmp
