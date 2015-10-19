target("docker", function()
  docker.build(".", {"-t", "flynn/slugbuilder"})
end)
