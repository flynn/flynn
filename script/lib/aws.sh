# Helpers for interacting with AWS services

check_aws_keys() {
  if [[ -z "${AWS_ACCESS_KEY_ID}" ]] || [[ -z "${AWS_SECRET_ACCESS_KEY}" ]]; then
    fail "Both AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set"
  fi
}

sync_cloudfront() {
  local src=$1
  local dst=$2

  # the sync command will output another command to check CloudFront
  # invalidation, so we need to capture that
  cf_cmd=$(s3cmd sync \
    --acl-public \
    --cf-invalidate \
    --no-preserve \
    --follow-symlinks \
    --no-mime-magic \
    "${src}" "${dst}" \
    | tee /dev/stderr \
    | grep -oP "s3cmd cfinvalinfo cf://\w+/\w+")

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
