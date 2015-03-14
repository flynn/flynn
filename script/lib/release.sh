# Release helpers

# new_release_manifest returns an empty release manifest.
new_release_manifest() {
    echo '{"versions":[]}'
}

# next_release_version reads a release manifest via STDIN and returns the next
# appropriate release version.
next_release_version() {
  local date=$(date +%Y%m%d)
  local iteration=$(jq --raw-output '.versions[].version' | grep "${date}" | wc -l)
  echo "${date}.${iteration}"
}
