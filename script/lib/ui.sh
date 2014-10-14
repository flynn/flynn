# Shell UI helpers

info() {
  local msg=$1
  local timestamp=$(date +%H:%M:%S.%3N)
  echo "===> ${timestamp} ${msg}"
}

fail() {
  local msg=$1
  info "ERROR: ${msg}" >&2
  exit 1
}
