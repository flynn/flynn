target("bin/flynn-postgres", function()
  go.build(".", {"-o", "bin/flynn-postgres"})
end)

target("bin/flynn-postgres-api", function()
  go.build("./api", {"-o", "bin/flynn-postgres-api"})
end)

target("docker", depends("bin/*"), function()
  docker.build(".", {"-t", "flynn/postgresql"})
end)


