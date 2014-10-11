# Helpers for interacting with AWS services

sync_cloudfront() {
  local src=$1
  local dst=$2

  # the sync command will output another command to check CloudFront invalidation, so we need to capture that
  cf_cmd=$(s3cmd sync --acl-public --cf-invalidate --no-preserve "${src}" "${dst}" | tee /dev/stderr | grep -oP 's3cmd cfinvalinfo cf://\w+/\w+')
  if [[ "${cf_cmd:0:5}" != "s3cmd" ]]; then
    fail "could not determine CloudFront invalidation command"
  fi

  info "waiting for CloudFront invalidation (can take ~10mins)"
  while true; do
    local status="$($cf_cmd | grep '^Status' | awk '{print $2}')"
    if [[ "${status}" = "Completed" ]]; then
      break
    fi
    info "invalidation status is currently ${status}, waiting 10s"
    sleep 10
  done
  info "CloudFront invalidation complete"
}
