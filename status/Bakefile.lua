target("bin/flynn-status", function()
  go.build(".", {"-o", "bin/flynn-status"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/status"})
end)
