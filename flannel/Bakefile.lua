target("bin/flanneld", function()
  go.build(".", {"-o", "bin/flanneld"})
end)

target("bin/flannel-wrapper", function()
  go.build("./wrapper", {"-o", "bin/flannel-wrapper"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/flannel"})
end)
