ImageRepository = "https://dl.flynn.io/tuf"
RootKeys = '[{"keytype":"ed25519","keyval":{"public":"6cfda23aa48f530aebd5b9c01030d06d02f25876b5508d681675270027af4731"}}]'

target("script/install-flynn", depends("host/bin/flynn-host.gz"), function()
  sh('sed "s/{{FLYNN-HOST-CHECKSUM}}/$(sha512sum host/bin/flynn-host.gz | cut -d " " -f 1)/g" script/install-flynn.tmpl > script/install-flynn')
end)

