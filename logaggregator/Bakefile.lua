target("bin/logaggregator", function() 
  go.build(".", {"-o", "bin/logaggregator"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/logaggregator"})
end)
