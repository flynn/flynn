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
  curl --silent "${BUILD_CACHE_URL}" | tar --extract --gunzip --directory "${cache_root}" &>/dev/null || true
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

# Fix for https://github.com/flynn/flynn/issues/85
export CURL_CONNECT_TIMEOUT=30

# Bump max time to download a single runtime tarball from its default of
# 30s (only makes sense on EC2) to 10 minutes
export CURL_TIMEOUT=600

## Buildpack detection

# Ordering here is in line number order from buildpacks.txt
buildpacks=(${buildpack_root}/*)
selected_buildpack=

if [[ -n "${BUILDPACK_URL}" ]]; then
  echo_title "Fetching custom buildpack"

  buildpack="${buildpack_root}/custom*"
  rm -rf "${buildpack}"
  /tmp/builder/install-buildpack \
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
  types=$(ruby -e "require 'yaml';
    puts YAML.load_file('${build_root}/Procfile').keys().join(', ')
  ")
  echo_normal "Procfile declares types -> ${types}"
fi
default_types=""
if [[ -s "${build_root}/.release" ]]; then
  default_types=$(ruby -e "require 'yaml';
    puts (YAML.load_file('${build_root}/.release')['default_process_types'] ||
          {}).keys.join(', ')
  ")
  [[ -n "${default_types}" ]] \
  && echo_normal \
    "Default process types for ${buildpack_name} -> ${default_types}"
fi


## Produce slug

if [[ -f "${build_root}/.slugignore" ]]; then
  tar \
    --exclude='.git' \
    --use-compress-program=pigz \
    -X "${build_root}/.slugignore" \
    -C ${build_root} \
    -cf ${slug_file} \
    . \
    | cat
else
  tar \
    --exclude='.git' \
    --use-compress-program=pigz \
    -C ${build_root} \
    -cf ${slug_file} \
    . \
    | cat
fi

if [[ "${slug_file}" != "-" ]]; then
  slug_size=$(du -Sh "${slug_file}" | cut -f1)
  echo_title "Compiled slug size is ${slug_size}"

  if [[ ${put_url} ]]; then
    curl -0 -s -o /dev/null -X PUT -T ${slug_file} "${put_url}"
  fi
fi

if [[ -n "${BUILD_CACHE_URL}" ]]; then
  tar \
    --create \
    --directory "${cache_root}" \
    --use-compress-program=pigz \
    . \
  | curl \
    --silent \
    --output /dev/null \
    --request PUT \
    --upload-file - \
    "${BUILD_CACHE_URL}"
fi
