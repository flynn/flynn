ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"

# load_lib sources a script from the script/lib directory.
load_lib() {
  local name=$1
  source "${ROOT}/script/lib/${name}"
}

# override date so that it's predictable in tests.
DATE="20150301"
date() {
  echo "${DATE}"
}

# assert_success tests that a `run` command sets $status to 0.
assert_success() {
  if [[ "${status}" -ne 0 ]]; then
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
