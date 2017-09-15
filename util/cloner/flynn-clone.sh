#!/usr/bin/env bash
#
# A script to clone a Flynn app.
#
# Typical usage would be:
#
#   flynn-clone.sh my-app my-app-staging
#
# This would create the my-app-staging app, copying the configuration
# from my-app.
#
# If the apps exist in different clusters, pass the --src-cluster and
# --dst-cluster flags:
#
#   flynn-clone.sh \
#     --src-cluster "production" \
#     --dst-cluster "staging" \
#     my-app \
#     my-app-staging
#
# It is recommended to only copy particular environment variables by
# specifying --env-whitelist which is a file containing a list of
# environment variables to copy, one per line:
#
#   # whitelist.env
#   KEY1
#   KEY2
#   KEY3
#
#   flynn-clone.sh \
#     --env-whitelist whitelist.env \
#     my-app \
#     my-app-staging

set -e

DEFAULT_SRC_CLUSTER="default"
DEFAULT_DST_CLUSTER="default"

usage() {
  cat >&2 <<USAGE
usage: $0 [options] SRC_APP DST_APP

Clone a Flynn app from SRC_APP to DST_APP.

OPTIONS:
  --src-cluster CLUSTER   Source cluster name [default: ${DEFAULT_SRC_CLUSTER}]
  --dst-cluster CLUSTER   Destination cluster name [default: ${DEFAULT_DST_CLUSTER}]
  --env-whitelist FILE    Environment variable whitelist file (whitespace separated list)
  -h, --help              Show this message
USAGE
}

main() {
  if ! which jq &>/dev/null; then
    fail "jq >= 1.5 is required: see https://stedolan.github.io/jq/download/"
  fi

  local src_app=""
  local dst_app=""
  local src_cluster="${DEFAULT_SRC_CLUSTER}"
  local dst_cluster="${DEFAULT_DST_CLUSTER}"
  local env_whitelist=""

  parse_args "$@"

  info "cloning source app \"${src_app}\" from \"${src_cluster}\" cluster to destination app \"${dst_app}\" in \"${dst_cluster}\" cluster"

  tmp="$(mktemp -d)"
  trap "rm -rf ${tmp}" EXIT

  info "getting source app"
  flynn -c "${src_cluster}" -a "${src_app}" release show --json > "${tmp}/src.json"

  info "generating destination release configuration"
  if [[ -n "${env_whitelist}" ]]; then
    info "filtering environment variables using whitelist:"
    cat "${env_whitelist}"

    # generate a whitelist JSON object like {key1: true, key2: true, ...}
    # and use it to filter both the release env and the individual process env
    jq \
      --raw-output \
      --argjson whitelist "$(jq --raw-input --slurp '[scan("\\S+")] | reduce .[] as $env ({}; .[$env] = true)' < "${env_whitelist}")" \
      '
        .env = (if(.env) then .env | with_entries(select(.key | in($whitelist))) else null end) |
        .processes |= (map_values(.env = (if(.env) then with_entries(select(.key | in($whitelist))) else null end)))
      ' \
      < "${tmp}/src.json" | \
      sed "s|${src_app}-\(.*\)-web|${dst_app}-\\1-web|g" | \
      sed "s|${src_app}-web|${dst_app}-web|g" \
      > "${tmp}/dst.json"
  else
    sed "s|${src_app}-\(.*\)-web|${dst_app}-\\1-web|g" "${tmp}/src.json" | \
    sed "s|${src_app}-web|${dst_app}-web|g" \
    > "${tmp}/dst.json"
  fi

  info "creating destination app"
  flynn -c "${dst_cluster}" -a "${dst_app}" create --remote "" "${dst_app}"

  info "creating destination release"
  # set an env var to create an inital release which can then be updated
  flynn -c "${dst_cluster}" -a "${dst_app}" env set "CLONE=true"
  flynn -c "${dst_cluster}" -a "${dst_app}" release update "${tmp}/dst.json"

  cat <<INFO

========================
Cloning finished at $(date +%H:%M:%S).

Database appliances have not been cloned, provision them now if
necessary with 'flynn -c ${dst_cluster} -a ${dst_app} resource add ...'.

Run 'flynn -c ${dst_cluster} -a ${dst_app} remote add <remote>' to add a git remote for
the new app and then 'git push <remote> master' to deploy the app.
========================

INFO
}

parse_args() {
  while true; do
    case "$1" in
      -h | --help)
        usage
        exit
        ;;
      --src-cluster)
        if [[ -z "$2" ]]; then
          usage
          fail "--src-cluster requires an argument"
        fi
        src_cluster="$2"
        shift 2
        ;;
      --dst-cluster)
        if [[ -z "$2" ]]; then
          usage
          fail "--dst-cluster requires an argument"
        fi
        dst_cluster="$2"
        shift 2
        ;;
      --env-whitelist)
        if [[ -z "$2" ]]; then
          usage
          fail "--env-whitelist requires an argument"
        fi
        env_whitelist="$2"
        shift 2
        ;;
      *)
        break
        ;;
    esac
  done

  if [[ $# -ne 2 ]]; then
    usage
    fail "invalid arguments"
  fi

  src_app="$1"
  dst_app="$2"
}

info() {
  echo "===> $(date +%H:%M:%S) $@"
}

fail() {
  echo "ERROR: $@" >&2
  exit 1
}

main "$@"
