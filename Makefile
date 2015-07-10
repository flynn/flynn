GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
GIT_TAG=`git describe --tags --exact-match --match "v*" 2>/dev/null || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`
GIT_DEV=GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false
GO_ENV=GOROOT=`readlink -f util/_toolchain/go`

all: toolchain
	@$(GIT_DEV) $(GO_ENV) tup

release: toolchain
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'

test: test-unit test-integration

test-unit-deps:
	@$(GIT_DEV) $(GO_ENV) tup appliance/etcd discoverd

test-unit: test-unit-deps
	@$(GO_ENV) PATH=${PWD}/appliance/etcd/bin:${PWD}/discoverd/bin:${PATH} util/_toolchain/go/bin/go test -race -cover ./...

test-unit-root: test-unit
	@$(GO_ENV) util/_toolchain/go/bin/go test -race -cover ./host/volume/zfs

test-integration:
	script/run-integration-tests

toolchain:
	@cd util/_toolchain && ./build.sh

.PHONY: all clean dev release test test-unit test-integration
