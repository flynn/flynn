#!/bin/bash
set -eo pipefail

if [[ "$1" == "-" ]]; then
  slug_file="$1"
else
  slug_file=/tmp/slug.tgz
  if [[ "$1" ]]; then
    put_url="$1"
  fi
fi

app_dir=/app
env_dir=/tmp/env
build_root=/tmp/build
cache_root=/tmp/cache
buildpack_root=/tmp/buildpacks
env_cookie=.ENV_DIR_bdca46b87df0537eaefe79bb632d37709ff1df18

mkdir -p ${app_dir}
mkdir -p ${cache_root}
mkdir -p ${buildpack_root}
mkdir -p ${build_root}/.profile.d

output_redirect() {
  if [[ "${slug_file}" == "-" ]]; then
    cat - 1>&2
  else
    cat -
  fi
}

echo_title() {
  echo $'\e[1G----->' $* | output_redirect
}

echo_normal() {
  echo $'\e[1G      ' $* | output_redirect
}

ensure_indent() {
  while read line; do
    if [[ "${line}" == --* ]]; then
      echo $'\e[1G'${line} | output_redirect
    else
      echo $'\e[1G      ' "${line}" | output_redirect
    fi
  done
}

run_unprivileged() {
  setuidgid nobody $@
}

# run curl silently and retry upto 3 times
curl() {
  $(which curl) --fail --silent --retry 3 $@
}

# removes leading and trailing whitespace
trim() {
  local var="$*"
  var="${var#"${var%%[![:space:]]*}"}"
  var="${var%"${var##*[![:space:]]}"}"
  echo -n "$var"
}

prune_slugignore() {
  shopt -s nullglob
  # read slugignore into array
  local globs=()
  local paths=()
  readarray -t globs < "${build_root}/.slugignore"
  # for line in slugignore
  for glob in ${globs[@]}; do
    # strip whitespace
    glob=$(trim ${glob})
    # ignore blank lines and comment lines
    if [[ ${glob} == "" ]] || [[ ${glob:0:1} == "#" ]]; then
      continue
    fi
    # remove leading slash(es)
    glob="${glob#"${glob%%[!"/"]*}"}"
    # append to build root and add to array of paths to remove
    paths=("${paths[@]}" ${build_root}/${glob})
  done
  echo_title "Deleting ${#paths[@]} files matching .slugignore patterns."
  rm -f ${paths[@]}
  shopt -u nullglob
}

cd ${app_dir}

## Load source from STDIN
cat | tar -xm


if [[ -f "${env_cookie}" ]]; then
  rm "${env_cookie}"
  mv app env /tmp
  rsync -aq /tmp/app/ .
  rm -rf /tmp/app
  envdir="true"
fi

if [[ -n "${BUILD_CACHE_URL}" ]]; then
  curl "${BUILD_CACHE_URL}" | tar --extract --gunzip --directory "${cache_root}" &>/dev/null || true
fi

# In heroku, there are two separate directories, and some
# buildpacks expect that.
cp -r . ${build_root}
chown -R nobody:nogroup ${app_dir} ${build_root} ${cache_root}

## Buildpack fixes

export APP_DIR="${app_dir}"
export HOME="${app_dir}"
usermod --home $HOME nobody
export REQUEST_ID="flynn-build"
export STACK=cedar-14
export CF_STACK=cflinuxfs2

# If there is a SSH private key available in the environment, save it so that it can be used
if [[ -n "${SSH_CLIENT_KEY}" ]]; then
  mkdir -p ${HOME}/.ssh
  file="${HOME}/.ssh/id_rsa"
  echo "${SSH_CLIENT_KEY}" > ${file}
  chown -R nobody:nogroup ${HOME}/.ssh
  chmod 600 ${file}
  unset SSH_CLIENT_KEY
fi

# If there is a list of known SSH hosts available in the environment, save it so that it can be used
if [[ -n "${SSH_CLIENT_HOSTS}" ]]; then
  mkdir -p ${HOME}/.ssh
  file="${HOME}/.ssh/known_hosts"
  echo "${SSH_CLIENT_HOSTS}" > ${file}
  chown -R nobody:nogroup ${HOME}/.ssh
  chmod 600 ${file}
  unset SSH_CLIENT_HOSTS
fi

# Fix for https://github.com/flynn/flynn/issues/85
export CURL_CONNECT_TIMEOUT=30

# Bump max time to download a single runtime tarball from its default of
# 30s (only makes sense on EC2) to 10 minutes
export CURL_TIMEOUT=600

# Remove files matched by .slugignore
if [[ -f "${build_root}/.slugignore" ]]; then
  prune_slugignore
fi

## Buildpack detection

# Ordering here is in line number order from buildpacks.txt
buildpacks=(${buildpack_root}/*)
selected_buildpack=

if [[ -n "${BUILDPACK_URL}" ]]; then
  echo_title "Fetching custom buildpack"

  buildpack="${buildpack_root}/custom*"
  rm -rf "${buildpack}"
  run_unprivileged /tmp/builder/install-buildpack \
    "${buildpack_root}" \
    "${BUILDPACK_URL}" \
    custom \
    "${env_dir}" \
    &> /dev/null
  buildpacks=($buildpack)
  selected_buildpack=${buildpack[0]}
  buildpack_name=$(run_unprivileged ${buildpack}/bin/detect "${build_root}")
else
  for buildpack in "${buildpacks[@]}"; do
    buildpack_name=$(run_unprivileged ${buildpack}/bin/detect "${build_root}") \
      && selected_buildpack="${buildpack}" \
      && break
  done
fi

if [[ -n "${selected_buildpack}" ]]; then
  echo_title "${buildpack_name} app detected"
else
  echo_title "Unable to select a buildpack"
  exit 1
fi

## Buildpack compile
if [[ -n "${envdir}" ]]; then
  run_unprivileged ${selected_buildpack}/bin/compile \
    "${build_root}" \
    "${cache_root}" \
    "${env_dir}" \
    | ensure_indent
else
  run_unprivileged ${selected_buildpack}/bin/compile \
    "${build_root}" \
    "${cache_root}" \
    | ensure_indent
fi

run_unprivileged ${selected_buildpack}/bin/release \
  "${build_root}" \
  "${cache_root}" \
  > ${build_root}/.release

## Display process types

echo_title "Discovering process types"
if [[ -f "${build_root}/Procfile" ]]; then
  types=$(ruby -r yaml -e "puts YAML.load_file('${build_root}/Procfile').keys.join(', ')")
  echo_normal "Procfile declares types -> ${types}"
fi
default_types=""
if [[ -s "${build_root}/.release" ]]; then
  default_types=$(ruby -r yaml -e "puts (YAML.load_file('${build_root}/.release') || {}).fetch('default_process_types', {}).keys.join(', ')")
  if [[ -n "${default_types}" ]]; then
    echo_normal "Default process types for ${buildpack_name} -> ${default_types}"
  fi
fi


## Produce slug
tar \
  --exclude='./.git' \
  --use-compress-program=pigz \
  -C ${build_root} \
  -cf ${slug_file} \
  . \
  | cat

if [[ "${slug_file}" != "-" ]]; then
  slug_size=$(du -Sh "${slug_file}" | cut -f1)
  echo_title "Compiled slug size is ${slug_size}"

  if [[ ${put_url} ]]; then
    curl -0 -o "$(mktemp)" -X PUT -T ${slug_file} "${put_url}"
  fi
fi

if [[ -n "${BUILD_CACHE_URL}" ]]; then
  tar \
    --create \
    --directory "${cache_root}" \
    --use-compress-program=pigz \
    . \
  | curl \
    --output "$(mktemp)" \
    --request PUT \
    --upload-file - \
    "${BUILD_CACHE_URL}"
fi
