tup.export("GOPATH")

for i, os in ipairs({"darwin", "linux"}) do
  for j, arch in ipairs({"amd64", "386"}) do
    tup.rule({"../util/release/flynn-release"},
             "^c go build %o^ GOOS="..os.." GOARCH="..arch.." ../util/_toolchain/go/bin/go build -o %o",
             {string.format("bin/flynn-%s-%s", os, arch)})
  end
end

tup.rule({"bin/flynn-linux-amd64"}, "cp %f %o", {"bin/flynn"})
