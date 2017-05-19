target("bin/flynn-receiver", depends("../gitreceive/flynn-receiver"), function()
  sh("cp ../gitreceive/flynn-receiver bin/flynn-receiver")
end)

target("bin/taffy", function()
  go.build(".", {"-o", "bin/taffy"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/taffy"})
end)
