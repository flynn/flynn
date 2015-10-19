target("bin/discoverd", function()
  go.build(".", {"-o", "bin/discoverd"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/discoverd"})
end)

