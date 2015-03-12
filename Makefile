GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
GIT_TAG=`git describe --tags --exact-match --match "v*" 2>/dev/null || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`

all: toolchain
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup

release: toolchain
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'

test: test-unit test-integration

test-unit:
	@GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false tup appliance/etcd discoverd
	go test ./...

test-integration:
	script/run-integration-tests

toolchain: util/_toolchain/go/bin/go

util/_toolchain/go/bin/go: util/_toolchain/bin/gonative
	cd util/_toolchain && rm -rf go && bin/gonative build -version=1.4.2

util/_toolchain/bin/gonative: Godeps/_workspace/src/github.com/inconshreveable/gonative/*.go
	go build -o util/_toolchain/bin/gonative github.com/flynn/flynn/Godeps/_workspace/src/github.com/inconshreveable/gonative


.PHONY: all clean dev release test test-unit test-integration
