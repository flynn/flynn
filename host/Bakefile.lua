target("root-keys", function()
  sh("sed s/{{TUF-ROOT-KEYS}}/" .. RootKeys .. "/g cli/root_keys.go.tmpl > cli/root_keys.go")
end)

target("bin/flynn-host", depends("root-keys"), function()
  go.build(".", {"-o", "bin/flynn-host"})
end)

target("bin/flynn-host.gz", depends("bin/flynn-host"), function()
  exec("gzip", "-f", "-9", "--keep", "bin/flynn-host")
end)

target("bin/flynn-init", function()
  go.build("./flynn-init", {"-o", "bin/flynn-init"})
end)

target("bin/flynn-nsumount", function()
  exec("gcc", "-o", "bin/flynn-nsumount", "-Wall", "-std=c99", "nsumount/nsumount.c")
end)
