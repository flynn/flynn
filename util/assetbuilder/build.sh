#!/bin/bash

mkdir /app
cp ${TARGET}/app/Gemfile      /app/Gemfile
cp ${TARGET}/app/Gemfile.lock /app/Gemfile.lock

apt-get update
apt-get install -y nodejs npm build-essential ruby ruby-dev
gem install bundler --no-rdoc --no-ri --version 1.17.3
cd /app
bundle install --deployment
mkdir --parents node_modules
chmod -R o+rw /app
