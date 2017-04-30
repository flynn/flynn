tup.export("GOPATH")
tup.export("GOROOT")
tup.export("GIT_COMMIT")
tup.export("GIT_BRANCH")
tup.export("GIT_TAG")
tup.export("GIT_DIRTY")

tup.rule({"tuf.go.tmpl"},
         "cat %f | sed 's|{{TUF-ROOT-KEYS}}|@(TUF_ROOT_KEYS)|' | sed 's|{{TUF-REPO}}|@(IMAGE_REPOSITORY)|' > %o",
         {"tuf.go"})

vpkg = "github.com/flynn/flynn/pkg/version"
for os, arches in pairs({darwin = {"amd64"}, freebsd = {"amd64"}, linux = {"amd64", "386"}, windows = {"amd64", "386"}}) do
  for j, arch in ipairs(arches) do
    tup.rule({"tuf.go"},
             "^c go build %o^ GOOS="..os.." GOARCH="..arch.." CGO_ENABLED=0  ../util/_toolchain/go/bin/go build -installsuffix nocgo -o %o -ldflags=\"-X "..vpkg..".commit=$GIT_COMMIT -X "..vpkg..".branch=$GIT_BRANCH -X "..vpkg..".tag=$GIT_TAG -X "..vpkg..".dirty=$GIT_DIRTY\"",
             {string.format("bin/flynn-%s-%s", os, arch)})
  end
end

tup.rule({"bin/flynn-linux-amd64"}, "cp %f %o", {"bin/flynn"})
