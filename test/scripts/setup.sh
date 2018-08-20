#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

APP="ci"
MEMORY="600G"

main() {
  info "getting controller key"
  local controller_key="$(flynn -a controller env get AUTH_KEY)"
  if [[ -z "${controller_key}" ]]; then
    fail "failed to get the controller key"
  fi

  info "creating app: ${APP}"
  flynn create --remote "" "${APP}" || true
  local app_id="$(controller_get "/apps/${APP}" | jq -r .id)"
  if [[ -z "${app_id}" ]]; then
    fail "failed to get app ID"
  fi

  info "creating test image"
  local image_id="$(create_test_image | jq -r .id)"
  if [[ -z "${image_id}" ]]; then
    fail "failed to create test image"
  fi

  info "creating test release"
  local release="$(create_test_release "${app_id}" "${image_id}" | jq -r .)"
  if [[ -z "${release}" ]]; then
    fail "failed to create test release"
  fi

  info "setting app release"
  set_app_release "${app_id}" "${release}" >/dev/null

  info "adding PostgreSQL database"
  if ! flynn -a "${APP}" resource | grep -q "postgres"; then
    flynn -a "${APP}" resource add postgres
  fi

  info "setting ${MEMORY} memory limit"
  flynn -a "${APP}" limit set runner "memory=${MEMORY}"

  cat <<EOF

The CI app is created, but you need to set the following environment
variables with 'flynn -a ${APP} env set KEY=VAL':

* AUTH_KEY
* BLOBSTORE_S3_CONFIG
* BLOBSTORE_GCS_CONFIG
* BLOBSTORE_AZURE_CONFIG
* GITHUB_TOKEN
* AWS_ACCESS_KEY_ID
* AWS_SECRET_ACCESS_KEY

Once set, scale up the 'runner' process with 'flynn -a ${APP} scale runner=1'.

EOF
  info "setup finished!"
}

create_test_image() {
  controller_post "/artifacts" "$(cat build/image/test.json)"
}

create_test_release() {
  local app_id=$1
  local image_id=$2

  controller_post "/releases" "$(new_release_json "${app_id}" "${image_id}")"
}

set_app_release() {
  local app_id=$1
  local release=$2

  controller_put "/apps/${app_id}/release" "${release}"
}

controller_get() {
  local path=$1

  controller_req "GET" "${path}"
}

controller_post() {
  local path=$1
  local data=$2

  controller_req "POST" "${path}" "${data}"
}

controller_put() {
  local path=$1
  local data=$2

  controller_req "PUT" "${path}" "${data}"
}

controller_req() {
  local method=$1
  local path=$2
  local data=$3

  local flags=(
    --request "${method}"
    --user    ":${controller_key}"
    --silent
    --fail
    --show-error
    --location
  )
  if [[ -n "${data}" ]]; then
    flags+=(
      --header      "Content-Type: application/json"
      --data-binary "${data}"
    )
  fi

  flynn -a blobstore run curl "${flags[@]}" "http://controller.discoverd${path}"
}

new_release_json() {
  local app_id=$1
  local image_id=$2

  cat <<JSON
{
  "app_id": "${app_id}",
  "artifacts": ["${image_id}"],
  "processes": {
    "runner": {
      "args": ["/test/bin/start-runner.sh"],
      "ports": [{
        "port": 80,
        "proto": "tcp",
        "service": {
          "name": "ci-web",
          "create": true
        }
      }],
      "mounts": [
        {
          "location":  "/opt/flynn-test",
          "target":    "/opt/flynn-test",
          "writeable": true
        }
      ],
      "profiles": ["loop"],
      "linux_capabilities": [
        "CAP_NET_RAW",
        "CAP_NET_BIND_SERVICE",
        "CAP_DAC_OVERRIDE",
        "CAP_SETFCAP",
        "CAP_SETPCAP",
        "CAP_SETGID",
        "CAP_SETUID",
        "CAP_MKNOD",
        "CAP_CHOWN",
        "CAP_FOWNER",
        "CAP_FSETID",
        "CAP_KILL",
        "CAP_SYS_CHROOT",
        "CAP_AUDIT_WRITE",
        "CAP_SYS_ADMIN"
      ]
    }
  }
}
JSON
}

main "$@"
