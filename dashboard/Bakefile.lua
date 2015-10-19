target("bin/go-bindata", function()
  go.build("../Godeps/_workspace/src/github.com/jteeuwen/go-bindata/go-bindata", {
    "-o", "bin/go-bindata",
  })
end)

target("app/compiler", function()
  go.build("./app", {"-o", "app/compiler"})
end)

target("assets", function()
  sh("cp ../installer/app/src/images/*.png app/lib/installer/images")
  sh("cp ../installer/app/src/views/*.js.jsx app/lib/installer/views")
  sh("cp ../installer/app/src/views/css/*.js app/lib/installer/views/css")
end)

target("bindata.go", depends("assets", "bin/go-bindata", "app/compiler"), function()
  exec("../util/assetbuilder/build.sh", "app", "dashboard")
end)

target("bin/flynn-dashboard", depends("bindata.go"), function()
  go.build("./dashboard", {"-o", "bin/flynn-dashboard"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/dashboard"})
end)
