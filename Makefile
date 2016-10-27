GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
# NOTE: the `git tag` command is filtered through `grep .` so it returns non-zero when empty
GIT_TAG=`git tag --list "v*" --sort "v:refname" --points-at HEAD 2>/dev/null | tail -n 1 | grep . || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`
GIT_DEV=GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false
GO_ENV=GOROOT=`readlink -f util/_toolchain/go`

all: toolchain docker
	@$(GIT_DEV) $(GO_ENV) tup

release: toolchain
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) $(GO_ENV) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'
	sudo rm -rf "/var/lib/flynn/layer-cache"

docker:
	sudo stop docker
	sudo rm -rf /var/lib/docker
	sudo apt-get update
	sudo apt-get install --yes --force-yes 'docker-engine=1.12.2-0~trusty'
	rm -rf log

test: test-unit test-integration

test-unit-deps: toolchain
	@$(GIT_DEV) $(GO_ENV) tup discoverd host/cli/root_keys.go installer/bindata.go dashboard/bindata.go

test-unit: test-unit-deps
	@# must use 'go list | grep' rather than ./... to avoid trying to build github.com/Microsoft/go-winio
	@# see https://github.com/Microsoft/go-winio/pull/33
	@$(GO_ENV) PATH=${PWD}/discoverd/bin:${PATH} util/_toolchain/go/bin/go test -race -cover `go list ./... | grep -vF 'github.com/flynn/flynn/vendor/github.com/Microsoft/go-winio'`

test-unit-root: test-unit
	@$(GO_ENV) util/_toolchain/go/bin/go test -race -cover ./host/volume/zfs ./pinkerton

test-integration: toolchain
	script/run-integration-tests

toolchain:
	@cd util/_toolchain && ./build.sh

.PHONY: all clean dev release test test-unit test-integration
