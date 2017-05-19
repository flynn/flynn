target("bin/flynn-blobstore", function()
  go.build(".", {"-o", "./bin/flynn-blobstore"})
end)

target("docker", depends("bin/flynn-blobstore"), function()
  docker.build(".", {"-t", "flynn/blobstore"})
end)
