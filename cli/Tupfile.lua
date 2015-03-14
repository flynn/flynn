tup.export("GOPATH")

tup.rule({"../util/rubyassetbuilder/*", "../util/cedarish/<docker>"},
          "^ docker build installer-builder^ cat ../log/docker-cedarish.log > /dev/null && ../util/rubyassetbuilder/build.sh image installer | tee %o",
          {"../log/docker-installer-builder.log", "<docker>"})
tup.rule("go build -o ../installer/bin/go-bindata ../Godeps/_workspace/src/github.com/jteeuwen/go-bindata/go-bindata",
          {"../installer/bin/go-bindata"})
tup.rule({"../installer/bin/go-bindata", "../log/docker-installer-builder.log"},
          "../util/rubyassetbuilder/build.sh app installer",
          {"../installer/bindata.go"})
for i, os in ipairs({"darwin", "linux"}) do
  for j, arch in ipairs({"amd64", "386"}) do
    tup.rule({"../installer/bindata.go", "../util/release/flynn-release"},
             "^c go build %o^ GOOS="..os.." GOARCH="..arch.." ../util/_toolchain/go/bin/go build -o %o",
             {string.format("bin/flynn-%s-%s", os, arch)})
  end
end

tup.rule({"bin/flynn-linux-amd64"}, "cp %f %o", {"bin/flynn"})
