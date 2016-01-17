#!/bin/bash

HOST=
while getopts :h: FLAG; do
  case $FLAG in
    h)
      HOST=$OPTARG
      ;;
  esac
done

# Send STDIN as body to redis process HTTP interface.
curl -X POST --data-binary @- http://$HOST:6380/restore
