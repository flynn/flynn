#!/bin/bash
## Load slug from Bind Mount, Artifacts dir or URL

set -eo pipefail

## Load profile.d, profile and release config

shopt -s nullglob
mkdir -p .profile.d
if [[ -s .release ]]; then
  ruby -r yaml > .profile.d/config_vars <<-RUBY
release = YAML.load_file('.release') || {}
config = release['config_vars'] || {}
config.each_pair do |k, v|
  puts "#{k}=\${#{k}:-'#{v}'}"
end
RUBY
fi
for file in .profile.d/*; do
  source "${file}"
done
if [[ -f .profile  ]]; then
  source .profile
fi
hash -r

## Inject "start" command to run processes defined in Procfile

case "$1" in
  start)
    if [[ -f Procfile ]]; then
      command=$(ruby -r yaml -e "puts YAML.load_file('Procfile')['$2']")
    else
      command=$(ruby -r yaml -e "puts (YAML.load_file('.release') || {}).fetch('default_process_types', {})['$2']")
    fi
    ;;
  *)
    printf -v command " %q" "$@"
    ;;
esac

## Run!

exec bash -c "${command}"
