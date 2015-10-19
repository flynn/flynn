target("flynn-receiver", function()
  go.build("./receiver", {"-o", "flynn-receiver"})
end)

target("gitreceived", depends("flynn-receiver"), function()
  go.build(".", {"-o", "gitreceived"})
end)

target("docker", depends("gitreceived"), function()
  docker.build(".", {"-t", "flynn/gitreceive"})
end)
