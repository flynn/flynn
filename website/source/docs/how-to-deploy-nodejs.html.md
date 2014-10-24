---
title: How To Deploy Node.js
layout: docs
---

# How To Deploy Node.js

Node.js is supported by the [Heroku Node.js buildpack](https://github.com/heroku/heroku-buildpack-nodejs).

## Detection

The Node.js buildpack is used if the repository contains a [`package.json`](https://www.npmjs.org/doc/files/package.json.html) file.

## Dependencies

Dependencies are managed using `npm`. `npm` expects dependencies specified under the [`dependencies` attribute](https://www.npmjs.org/doc/files/package.json.html#dependencies) inside the `package.json` file, which is just a simple object, with package names as the keys, mapping to version ranges.

### Specifying a Node.js Version

Node.js version can be specified using the [`engines` section](https://www.npmjs.org/doc/files/package.json.html#engines) of the `package.json` file. It uses [semver.io](http://semver.io/) to resolve the node version, so queries in the format of `0.8.x`, `>0.4`, `>=0.8.5 <=0.8.14` are supported. This buildpack can run any Node.js version past 0.8.5, including development versions.

### Example package.json

```json
{
  "name": "node-example",
  "version": "0.0.1",
  "dependencies": {
    "express": "4.10.0",
    "stylus": "0.49.2"
  },
  "devDependencies": {
    "grunt": "0.4.5"
  },
  "engines": {
    "node": "0.10.x",
    "npm": "1.2.x"
  }
}
```

## Custom Build Step

For apps that require extra processing before the deploy, an npm `postinstall` script can be added. It will run immediately after `npm install --production`, and the release environment will be available. Note that the buildpack doesn't install `devDependencies`. Should you require any of those, they should be moved to `dependencies`.

## Default Process Type

Node.js apps can be deployed without a `Procfile`. If no `Procfile` is present, the buildpack will expect a [`scripts.start`](https://www.npmjs.org/doc/misc/npm-scripts.html) key in the `package.json` file, and the default process type `web` will run the script using `npm start`.

## Run Jobs

Besides the usual utilities, `npm` and `node` are in `PATH` and are available directly via `flynn run`.

```
$ flynn run node -v
v0.10.32
```
