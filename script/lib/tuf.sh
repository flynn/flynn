download_tuf_metadata() {
  local tuf_dir=$1
  local bucket=$2

  mkdir -p "${tuf_dir}/repository"
  for role in "root" "targets" "snapshot" "timestamp"; do
    s3cmd get --force "s3://${bucket}/tuf/${role}.json" "${tuf_dir}/repository/${role}.json"
  done
}

metadata_expires_before() {
  local path=$1
  local before=$2

  local expires="$(cat "${path}" | jq --raw-output .signed.expires)"
  if [[ -z "${expires}" ]]; then
    fail "unable to determine expires"
  fi

  if [[ $(date --date "${expires}" +%s) -le $(date --date "${before}" +%s) ]]; then
    return 0
  fi

  return 1
}

check_tuf_keys() {
  local tuf_dir=$1

  info "checking TUF signing keys"
  for role in "targets" "snapshot" "timestamp"; do
    if [[ ! -s "${tuf_dir}/keys/${role}.json" ]]; then
      fail "the TUF repo is missing the ${role} signing key"
    fi
  done
}

release_exists() {
  local tuf_dir=$1
  local version=$2

  if jq --exit-status ".signed.targets.\"/${version}/flynn-host.gz\"" "${tuf_dir}/repository/targets.json" &>/dev/null; then
    return 0
  else
    return 1
  fi
}
