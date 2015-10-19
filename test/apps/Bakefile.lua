target("bin/echoer", function()
  go.build("./echoer", {"-o", "bin/echoer"})
end)

target("bin/ping", function()
  go.build("./ping", {"-o", "bin/ping"})
end)

target("bin/signal", function()
  go.build("./signal", {"-o", "bin/signal"})
end)

target("bin/ish", function()
  go.build("./ish", {"-o", "bin/ish"})
end)

target("bin/partial-logger", function()
  go.build("./partial-logger", {"-o", "bin/partial-logger"})
end)

target("docker", depends("bin/*"), function() 
    docker.build(".", {"-t", "flynn/test-apps"})
end)
