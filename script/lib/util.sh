# Various utility functions

sha256() {
  local file=$1
  sha256sum "${file}" | cut -d " " -f 1
}
