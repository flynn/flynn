#!/bin/bash
set -eo pipefail

if [ $# -ne 4 ]
then
  echo "Usage: `basename $0` <app> <repo> <branch> <rev>"
  exit 65
fi

APP="$1"
REPO="$2"
BRANCH="$3"
REV="$4"

DEST=/tmp/app
git clone --depth=50 --branch="$BRANCH" "$REPO" $DEST
cd $DEST
git archive $REV | /bin/flynn-receive "$APP" "$REV"
