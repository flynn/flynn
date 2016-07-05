# Shell UI helpers

# say prints the given message to STDOUT, using the optional color if
# STDOUT is a terminal.
#
# usage:
#
#   say "foo"              - prints "foo"
#   say "bar" "red"        - prints "bar" in red
#   say "baz" "green"      - prints "baz" in green
#   say "qux" "red" | tee  - prints "qux" with no colour
#
say() {
  local msg=$1
  local color=$2

  if [[ -n "${color}" ]] && [[ -t 1 ]]; then
    case "${color}" in
      red)
        echo -e "\e[1;31m${msg}\e[0m"
        ;;
      green)
        echo -e "\e[1;32m${msg}\e[0m"
        ;;
      yellow)
        echo -e "\e[1;33m${msg}\e[0m"
        ;;
      *)
        echo "${msg}"
        ;;
    esac
  else
    echo "${msg}"
  fi
}

info() {
  local msg=$1
  local timestamp=$(date +%H:%M:%S.%3N)
  say "===> ${timestamp} ${msg}" "green"
}

warn() {
  local msg=$1
  local timestamp=$(date +%H:%M:%S.%3N)
  say "===> ${timestamp} WARN: ${msg}" "yellow" >&2
}

fail() {
  local msg=$1
  say "ERROR: ${msg}" "red" >&2
  exit 1
}
