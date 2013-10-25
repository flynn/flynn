
build:
	go build

install: build
	cp shelf /usr/local/bin