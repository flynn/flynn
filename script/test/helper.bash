ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"

# load_lib sources a script from the script/lib directory.
load_lib() {
  local name=$1
  source "${ROOT}/script/lib/${name}"
}

# assert_success tests that a `run` command sets $status to 0.
assert_success() {
  if [[ "${status}" -ne 0 ]]; then
    echo "unexpected exit status: ${status}"
    return 1
  fi
}

# assert_failure tests that a `run` command does not set $status to 0.
assert_failure() {
  if [[ "${status}" -eq 0 ]]; then
    echo "unexpected exit status: ${status}"
    return 1
  fi
}

# assert_output tests that a `run` command sets $output to an expected value.
assert_output() {
  local expected=$1
  if [[ "${output}" != "${expected}" ]]; then
    echo "expected: ${expected}"
    echo "actual:   ${output}"
    return 1
  fi
}
