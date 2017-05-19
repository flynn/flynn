target("bin/flynn-controller", function()
  go.build(".", {"-o", "bin/flynn-controller"})
end)

target("bin/flynn-scheduler", function()
  go.build("./scheduler", {"-o", "bin/flynn-scheduler"})
end)

target("bin/flynn-worker", function()
  go.build("./worker", {"-o", "bin/flynn-worker"})
end)

target("copy-schema", function()
  sh("cp ../schema/*.json bin/jsonschema")
  sh("cp ../schema/controller/*.json bin/jsonschema/controller")
  sh("cp ../schema/router/*.json bin/jsonschema/router")
end)

target("examples/flynn-controller-examples", function()
  go.build("./examples", {"-o", "examples/flynn-controller-examples"})
end)

target("examples/docker", depends("examples/flynn-controller-examples"), function()
  docker.build("examples", {"-t", "flynn/controller-examples"})
end)

target("docker", depends("bin/*", "copy-schema", "examples/flynn-controller-examples"), function()
  docker.build(".", {"-t", "flynn/controller"})
end)
