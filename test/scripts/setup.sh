#!/bin/bash

set -e

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
source "${ROOT}/script/lib/ui.sh"

# DEFAULT_CLUSTER is the default Flynn cluster that CI will be setup in (can be
# overridden by setting FLYNN_CI_CLUSTER)
DEFAULT_CLUSTER="flynn-ci"

# DEFAULT_APP is the default Flynn app that CI will be setup in (can be
# overridden by setting FLYNN_CI_APP)
DEFAULT_APP="ci"

# DEFAULT_MEMORY is the default memory limit for the CI runner job (can be
# overridden by setting FLYNN_CI_MEMORY)
DEFAULT_MEMORY="600G"

main() {
  export FLYNN_CLUSTER="${FLYNN_CI_CLUSTER:-${DEFAULT_CLUSTER}}"

  local app="${FLYNN_CI_APP:-${DEFAULT_APP}}"

  info "getting controller key"
  local controller_key="$(flynn -a controller env get AUTH_KEY)"
  if [[ -z "${controller_key}" ]]; then
    fail "failed to get the controller key"
  fi

  info "creating app: ${app}"
  flynn create --remote "" "${app}" || true
  local app_id="$(controller_get "/apps/${app}" | jq -r .id)"
  if [[ -z "${app_id}" ]]; then
    fail "failed to get app ID"
  fi

  info "uploading image layers"
  while read layer_id; do
    upload_layer "${layer_id}" < /dev/null
  done < <(jq -r '.manifest.rootfs[].layers[].id' "build/image/test.json")

  info "creating test image"
  local image_id="$(create_test_image | jq -r .id)"
  if [[ -z "${image_id}" ]]; then
    fail "failed to create test image"
  fi

  # if the app already has a release just update the image and deploy
  if flynn -a "${app_id}" release show &>/dev/null; then
    info "updating release image"
    flynn -a controller pg psql -- -c "UPDATE release_artifacts SET artifact_id = '${image_id}' WHERE release_id = (SELECT release_id FROM apps WHERE app_id = '${app_id}')"

    info "deploying release"
    flynn -a "${app_id}" env set RESTART=1

    return
  fi

  info "creating test release"
  local release_id="$(create_test_release "${app_id}" "${image_id}" | jq -r .id)"
  if [[ -z "${release_id}" ]]; then
    fail "failed to create test release"
  fi

  info "setting app release"
  set_app_release "${app_id}" "${release_id}" >/dev/null

  info "adding PostgreSQL database"
  flynn -a "${app}" resource add postgres

  local memory="${FLYNN_CI_MEMORY:-${DEFAULT_MEMORY}}"
  info "setting ${memory} memory limit"
  flynn -a "${app}" limit set runner "memory=${memory}"

  cat <<EOF

The CI app is created, but you need to set the following environment
variables with 'flynn -c ${FLYNN_CLUSTER} -a ${app} env set KEY=VAL':

* AUTH_KEY
* BLOBSTORE_S3_CONFIG
* BLOBSTORE_GCS_CONFIG
* BLOBSTORE_AZURE_CONFIG
* GITHUB_TOKEN
* AWS_ACCESS_KEY_ID
* AWS_SECRET_ACCESS_KEY

Once set, scale up the 'runner' process with 'flynn -c ${FLYNN_CLUSTER} -a ${app} scale runner=1'.

EOF
  info "setup finished!"
}

upload_layer() {
  local layer_id=$1

  local layer="/var/lib/flynn/layer-cache/${layer_id}.squashfs"
  if [[ ! -s "${layer}" ]]; then
    fail "missing layer: ${layer}"
  fi

  info "uploading layer ${layer_id}"
  local url="http://blobstore.discoverd/ci/layers/${layer_id}.squashfs"
  if ! flynn -a blobstore run curl --silent --head "${url}" | grep -qF "HTTP/1.1 200 OK"; then
    flynn -a blobstore run curl -X PUT --data-binary @- "${url}" < "${layer}"
  fi
}

create_test_image() {
  controller_post \
    "/artifacts" \
    "$(jq '.layer_url_template = "http://blobstore.discoverd/ci/layers/{id}.squashfs"' build/image/test.json)"
}

create_test_release() {
  local app_id=$1
  local image_id=$2

  controller_post "/releases" "$(new_release_json "${app_id}" "${image_id}")"
}

set_app_release() {
  local app_id=$1
  local release_id=$2

  controller_put "/apps/${app_id}/release" "$(jq -n --arg id "${release_id}" '{ $id }')"
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
