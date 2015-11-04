# Release helpers

# next_release_version takes the previous release version
# and returns the next one
next_release_version() {
  local previous=$1
  local date=$(date +%Y%m%d)
  local iteration
  if [[ $previous =~ $date ]]; then
    previous_iteration="${previous##*.}"
    iteration=$((previous_iteration+1))
  else
    iteration=0
  fi
  echo "v${date}.${iteration}"
}
