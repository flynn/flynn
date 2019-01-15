#!/bin/bash

mkdir /app
cp ${TARGET}/app/Gemfile      /app/Gemfile
cp ${TARGET}/app/Gemfile.lock /app/Gemfile.lock

# Install NodeSource repo and nodejs, install then run bundler and download npm deps
curl --fail --silent --location https://deb.nodesource.com/setup_4.x | bash -
apt-get update
apt-get install -y nodejs build-essential ruby ruby-dev
gem install bundler --no-rdoc --no-ri --version 1.17.3
cd /app
bundle install --deployment
mkdir --parents node_modules
npm install recast@0.10.30 es6-promise@3.0.2 node-sass@3.3.3 react-tools@0.13
chmod -R o+rw /app
