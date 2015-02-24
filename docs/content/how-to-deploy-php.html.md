---
title: How To Deploy PHP
layout: docs
---

# How To Deploy PHP

Flynn supports deploying PHP applications using either the PHP or [HHVM](http://hhvm.com/)
runtime, with a choice of either [Apache2](http://httpd.apache.org/) or
[Nginx](http://wiki.nginx.org/Main) web server.

Flynn uses the [PHP buildpack](https://github.com/heroku/heroku-buildpack-php) to detect,
compile, and release PHP applications.

## Detection

Flynn detects a PHP application by the presence of a `composer.json` file in the root directory.
It then uses [Composer](https://getcomposer.org/) (A PHP dependency management tool) to
determine, download, and install the dependencies of the application.

If an application has no Composer dependencies, it should still include an empty `composer.json`
file to be detected as a PHP application.

## Dependencies

### Packages

The primary use of the `composer.json` file is to declare package dependencies.

Here is an example `composer.json` file to declare a dependency on the
[monolog](https://github.com/Seldaek/monolog) package:

```json
{
  "require": {
    "monolog/monolog": "1.11.*"
  }
}
```

Running `composer install` will download and install the necessary dependencies and
create a `composer.lock` file. This contains a snapshot of all the packages and versions
which were installed. The `composer.lock` file must be present when the application is
deployed so that Flynn can determine the exact versions of packages to install.

Composer also creates a `vendor/autoload.php` file which can be required, leading to
classes from the dependent packages being autoloaded. So to use the `monolog` package
in your application:

```php
require 'vendor/autoload.php';

// you can now reference Monolog
use Monolog\Logger;

$log = new Logger('my-application');
...
```

*For more information on the `composer.json` format, see the [Composer JSON schema page]
(https://getcomposer.org/doc/04-schema.md).*

### Environment-Specific

There are some packages you may use locally that don't need to be installed in
production. Flynn calls `composer install` with `--no-dev`, which will skip
installing any packages in the `require-dev` property of `composer.json`.

For example, you may use the `phpunit` package locally, but this is seldom
required in production, so you should put this into the `require-dev` property:

```json
{
  "require": {
    "monolog/monolog": "1.11.*"
  },
  "require-dev": {
    "phpunit/phpunit": "4.3.*"
  }
}

```

For reference, here is the full Composer command that will be run:

```
composer install \
  --no-dev \
  --prefer-dist \
  --optimize-autoloader \
  --no-interaction
```

*For more information on the `composer install` command, see the [composer install page](https://getcomposer.org/doc/03-cli.md#install).*

### PHP Runtime

By default, applications will run on the latest stable PHP runtime, A specific version
of either the PHP or HHVM runtime can be used by specifying an appropriate dependency on
either the `php` or `hhvm` package respectively.

To enable PHP 5.6.x:

```json
{
  "require": {
    "php": "~5.6.0"
  }
}
```

To enable HHVM 3.2.x:

```json
{
  "require": {
    "hhvm": "~3.2.0"
  }
}
```

*It is advisable to use the `~` operator when specifying versions so your application is
always running on the latest stable minor release. For more information on the `~` operator,
see [this Composer guide](https://getcomposer.org/doc/01-basic-usage.md#next-significant-release-tilde-operator-).*

## Process Types

The type of processes that your application supports can be declared in a `Procfile` in the
root directory, which contains a line per type in the format `TYPE: COMMAND`.

### web

The `web` process type gets an allocated HTTP route and a corresponding `PORT` environment
variable, so it typically starts an HTTP server for your application.

Flynn supports two web servers out-of-the-box:

#### Apache2

To start Apache2 (and PHP-FPM), use the `heroku-php-apache2` script:

```
web: vendor/bin/heroku-php-apache2
```

If you are using the HHVM runtime, use the `heroku-hhvm-apache2` script:

```
web: vendor/bin/heroku-hhvm-apache2
```

#### Nginx

To start Nginx (and PHP-FPM), use the `heroku-php-nginx` script:

```
web: vendor/bin/heroku-php-nginx
```

If you are using the HHVM runtime, use the `heroku-hhvm-nginx` script:

```
web: vendor/bin/heroku-hhvm-nginx
```

#### Default

If your application does not have a `Procfile`, a default `web` process will be used
to start Apache with the runtime defined in `composer.json` (i.e. either
`vendor/bin/heroku-php-apache2` or `vendor/bin/heroku-hhvm-apache2`).
