
build:
	GOPATH=. cd gitreceived && go get || true && go build

install: build
	cp gitreceived/gitreceived /usr/local/bin