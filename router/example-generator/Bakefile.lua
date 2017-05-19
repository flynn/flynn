target("flynn-router-examples", function()
  go.build(".", {"-o", "flynn-router-examples"})
end)

target("docker", depends("flynn-router-examples"), function() 
  docker.build(".", {"-t", "flynn/router-examples", })
end)

