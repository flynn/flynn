target("updater", function()
  go.build(".", {"-o", "updater"})
end)

target("docker", depends("updater"), function()
  docker.build(".", {"-t", "flynn/updater"})
end)
