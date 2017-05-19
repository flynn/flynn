target("@docker", function()
  docker.build(".", {"-t", "flynn/slugrunner"})
end)
