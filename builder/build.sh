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
build_root=/tmp/build
cache_root=/tmp/cache
buildpack_root=/tmp/buildpacks

mkdir -p $app_dir
mkdir -p $cache_root
mkdir -p $buildpack_root
mkdir -p $build_root/.profile.d

function output_redirect() {
	if [[ "$slug_file" == "-" ]]; then
		cat - 1>&2
	else
		cat -
	fi
}

function echo_title() {
  echo $'\e[1G----->' $* | output_redirect
}

function echo_normal() {
  echo $'\e[1G      ' $* | output_redirect
}

function ensure_indent() {
  while read line; do
    if [[ "$line" == --* ]]; then
      echo $'\e[1G'$line | output_redirect
    else
      echo $'\e[1G      ' "$line" | output_redirect
    fi

  done 
}

## Load source from STDIN

cat | tar -xC $app_dir

# In heroku, there are two separate directories, and some
# buildpacks expect that.
cp -r $app_dir/. $build_root

## Buildpack fixes

export REQUEST_ID=$(openssl rand -base64 32)
export APP_DIR="$app_dir"
export HOME="$app_dir"

## Buildpack detection

buildpacks=($buildpack_root/*)
selected_buildpack=

if [[ -n "$BUILDPACK_URL" ]]; then
	echo_title "Fetching custom buildpack"

	buildpack="$buildpack_root/custom"
	rm -fr "$buildpack"
	git clone --depth=1 "$BUILDPACK_URL" "$buildpack"
	selected_buildpack="$buildpack"
	buildpack_name=$($buildpack/bin/detect "$build_root") && selected_buildpack=$buildpack
else
    for buildpack in "${buildpacks[@]}"; do
    	buildpack_name=$($buildpack/bin/detect "$build_root") && selected_buildpack=$buildpack && break
    done
fi

if [[ -n "$selected_buildpack" ]]; then
	echo_title "$buildpack_name app detected"
	else
	echo_title "Unable to select a buildpack"
	exit 1
fi

## Buildpack compile

$selected_buildpack/bin/compile "$build_root" "$cache_root" | ensure_indent

$selected_buildpack/bin/release "$build_root" "$cache_root" > $build_root/.release

## Display process types

echo_title "Discovering process types"
if [[ -f "$build_root/Procfile" ]]; then
	types=$(ruby -e "require 'yaml';puts YAML.load_file('$build_root/Procfile').keys().join(', ')")
	echo_normal "Procfile declares types -> $types"
fi
default_types=""
if [[ -s "$build_root/.release" ]]; then
	default_types=$(ruby -e "require 'yaml';puts (YAML.load_file('$build_root/.release')['default_process_types'] || {}).keys().join(', ')")
	[[ $default_types ]] && echo_normal "Default process types for $buildpack_name -> $default_types"
fi


## Produce slug

if [[ -f "$build_root/.slugignore" ]]; then
	tar --exclude='.git' -X "$build_root/.slugignore" -C $build_root -czf $slug_file . | cat
else
	tar --exclude='.git' -C $build_root -czf $slug_file . | cat
fi
  
if [[ "$slug_file" != "-" ]]; then
	slug_size=$(du -Sh /tmp/slug.tgz | cut -f1)
	echo_title "Compiled slug size is $slug_size"

	if [[ $put_url ]]; then
		curl -0 -s -o /dev/null -X PUT -T $slug_file "$put_url" 
	fi
fi
