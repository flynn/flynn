GIT_COMMIT=`git rev-parse --short HEAD`
GIT_BRANCH=`git rev-parse --abbrev-ref HEAD`
# NOTE: the `git tag` command is filtered through `grep .` so it returns non-zero when empty
GIT_TAG=`git tag --list "v*" --sort "v:refname" --points-at HEAD 2>/dev/null | tail -n 1 | grep . || echo "none"`
GIT_DIRTY=`test -n "$(git status --porcelain)" && echo true || echo false`
GIT_DEV=GIT_COMMIT=dev GIT_BRANCH=dev GIT_TAG=none GIT_DIRTY=false
GO_ENV=GOROOT=`readlink -f util/_toolchain/go`
BUILD_ENV=FLYNN_HOST_ADDR="192.0.2.100:1113"

all: toolchain dashboard-assets
	@$(GIT_DEV) $(GO_ENV) $(BUILD_ENV) tup

release: toolchain dashboard-assets
	@GIT_COMMIT=$(GIT_COMMIT) GIT_BRANCH=$(GIT_BRANCH) GIT_TAG=$(GIT_TAG) GIT_DIRTY=$(GIT_DIRTY) $(GO_ENV) $(BUILD_ENV) tup

clean:
	git clean -Xdf -e '!.tup' -e '!.vagrant' -e '!script/custom-vagrant'
	sudo rm -rf "/var/lib/flynn/layer-cache"

# copy the static installer assets to the dashboard here rather than in tup
# to avoid tup complaining about generated directories
dashboard-assets:
	@mkdir -p dashboard/app/lib/installer/images dashboard/app/lib/installer/views/css
	@cp installer/app/src/images/*.png dashboard/app/lib/installer/images
	@cp installer/app/src/views/*.js.jsx dashboard/app/lib/installer/views
	@cp installer/app/src/views/css/*.js dashboard/app/lib/installer/views/css

test: test-unit test-integration

test-unit-deps: toolchain
	@$(GIT_DEV) $(GO_ENV) $(BUILD_ENV) tup discoverd host/cli/root_keys.go installer/bindata.go dashboard/bindata.go

test-unit:
	@$(GO_ENV) PATH=${PWD}/discoverd/bin:${PATH} util/_toolchain/go/bin/go test -race -cover ./...

test-unit-root: test-unit
	@sudo -E $(GO_ENV) util/_toolchain/go/bin/go test -race -cover ./host/volume/...

test-integration: toolchain
	script/run-integration-tests

toolchain:
	@git clean -Xdf
	@cd util/_toolchain && ./build.sh

.PHONY: all clean dev release test test-unit test-integration
