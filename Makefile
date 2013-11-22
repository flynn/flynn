VERSION = 0.1.0

build: shelf

shelf:
	go build

container: shelf
	docker build -t flynn/shelf .

install: build
	cp shelf /usr/local/bin