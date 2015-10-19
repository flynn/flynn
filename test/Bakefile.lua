target("bin/flynn-test", function()
  go.build(".", {"-o", "bin/flynn-test"})
end)

target("bin/flynn-test-runner", function()
  go.build("./runner", {"-o", "bin/flynn-test-runner"})
end)
