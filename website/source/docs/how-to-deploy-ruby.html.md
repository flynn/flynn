---
title: How To Deploy Ruby
layout: docs
---

# How to Deploy Ruby

Flynn supports deploying Ruby, Rack, and Rails applications using a variety of Ruby
interpreters, namely MRI, [JRuby](http://www.jruby.org), and [Rubinius](http://rubini.us).

Flynn uses the [Heroku Ruby buildpack](https://github.com/heroku/heroku-buildpack-ruby)
to detect, compile, and release Ruby applications.

## Detection

Flynn detects that an application requires Ruby by the presence of a `Gemfile` in
the root directory. It then uses [Bundler](https://bundler.io) (a Ruby dependency
management tool) to determine, download, and install the dependencies of the application.

## Dependencies

### Gems

The primary use of the `Gemfile` is to declare gem dependenices.

Here is an example `Gemfile` which could be used to deploy a Ruby application which
depends on the `rack` gem:

```ruby
source "https://rubygems.org"

gem "rack"
```

Given this `Gemfile` in your application, you would run `bundle install` locally
to install the necessary dependencies, and Bundler would write a snapshot of all
of the gems and versions that it installed to `Gemfile.lock`.

The `Gemfile.lock` needs to be present when you deploy your application so that
Flynn can determine what gems to install. **The deployment
will fail if this file is missing**.

*For more information on the `Gemfile` format, see the [Bundler Gemfile page](http://bundler.io/gemfile.html).*

#### Environment-Specific

There are some gems you may use locally that don't need to be installed in
production. Flynn calls `bundle install` with `--without development:test`, which
will skip installing any gems in the `development` or `test` groups in
the `Gemfile`.

For example, you may use the `debugger` and `rspec` gems locally, but these are seldom
required in production, so you should put these into groups:

```ruby
group :development do
  gem "debugger"
end

group :test do
  gem "rspec"
end
```

For reference, here is the full Bundler command that will be run:

```
bundle install \
  --without development:test \
  --path vendor/bundle \
  --binstubs vendor/bundle/bin \
  -j4 \
  --deployment \
  --no-clean
```

*For more information on the `bundle install` command, see the [bundle install page](http://bundler.io/bundle_install.html).*

### Ruby Interpreter

If your application requires a particular Ruby interpreter and version, you can specify
that using `ruby` in the `Gemfile`.

To use MRI v2.1.2:

```
ruby "2.1.2"
```

To use JRuby 1.7.16 with Ruby 2.0 support:

```
ruby "2.0.0", engine: "jruby", engine_version: "1.7.16"
```

To use Rubinius 2.2.10 with Ruby 2.1 support:

```
ruby "2.1.0", engine: "rbx", engine_version: "2.2.10"
```

### Native Libraries

The container in which your application is built and run includes a number of pre-installed
libraries that are useful when compiling Ruby native extensions, for example:

* `libssl-dev`
* `libmysqlclient-dev`
* `libxml2-dev`
* `libxslt-dev`

*For a full list, see the [base image Dockerfile](https://github.com/flynn/flynn/blob/master/util/cedarish/Dockerfile).*

## Process Types

The type of processes that your application supports can be declared in a `Procfile` in the
root directory, which contains a line per type in the format `TYPE: COMMAND`.

If your application does not have a `Procfile`, default process types will be assigned
(see [Framework Detection](#framework-detection) below for more information).

The following are some common process types:

### web

The `web` process type gets an allocated HTTP route and a corresponding `PORT` environment
variable, so it typically starts an HTTP server for your application. Examples include:

#### [Thin](http://code.macournoyer.com/thin/)

```
web: bundle exec thin start -p $PORT -e $RACK_ENV
```

#### [Unicorn](http://unicorn.bogomips.org/)

```
web: bundle exec -p $PORT -c config/unicorn.rb
```

#### [Puma](http://puma.io/)

```
web: bundle exec puma -C config/puma.rb
```

*Note: When using Puma, ensure you use `ENV["PORT"]` as the port in `config/puma.rb`*

### worker

The `worker` process type commonly starts a background job processor which works off
jobs in a queue. Examples include:

#### [Resque](https://github.com/resque/resque)

```
worker: QUEUE=* bundle exec rake resque:work
```

#### [Sidekiq](http://sidekiq.org/)

```
worker: bundle exec sidekiq
```

#### [Delayed::Job](https://github.com/collectiveidea/delayed_job)

```
worker: bundle exec delayed_job start
```

### clock

The `clock` process type is commonly used to start a cron-like process to execute
code at certain intervals.

The [Clockwork](https://github.com/tomykaira/clockwork) gem provides a nice DSL, and would
be declared as a process type like so:

```
clock: bundle exec clockwork lib/clock.rb
```

where `lib/clock.rb` contains your Clockwork declarations.

### Testing Locally

If you want to test your process types locally, you should install the
[Foreman](https://github.com/ddollar/foreman) gem, add required environment
variables to `.env` in the root of your project (e.g. `PORT=5000`), then start
Foreman:

```
$ foreman start
14:25:33 web.1    | started with pid 42868
14:25:33 worker.1 | started with pid 42869
14:25:33 clock.1  | started with pid 42870
14:25:34 web.1    | == Sinatra/1.4.5 has taken the stage on 5000 for development with backup from Thin
14:25:34 web.1    | Thin web server (v1.6.3 codename Protein Powder)
14:25:34 web.1    | Maximum connections set to 1024
14:25:34 web.1    | Listening on localhost:5000, CTRL+C to stop
14:25:34 clock.1  | I, [2014-10-24T14:25:34.729860 #42870]  INFO -- : Starting clock for 1 events: [ frequent.job ]
14:25:34 clock.1  | I, [2014-10-24T14:25:34.729999 #42870]  INFO -- : Triggering 'frequent.job'
14:25:34 clock.1  | Running frequent.job
...
```

## Framework Detection

Different Ruby frameworks require different configuration, so Flynn will detect the presence
of a framework and configure it as required.

Here are the rules Flynn uses to detect various frameworks, along with their default process
types (i.e. the process types if there is no `Procfile` present):

### Ruby

Presence of `Gemfile` in the root directory indicates this is a Ruby application.

Default process types:

```
rake: bundle exec rake
console: bundle exec irb
```

### Rack

Presence of the `rack` gem in `Gemfile.lock` indicates this is a Rack application.

Default process types:

```
web: bundle exec rackup config.ru -p $PORT
rake: bundle exec rake
console: bundle exec irb
```

### Rails 2

Presence of the `rails` gem with a version >= 2.0.0 and < 3.0.0 in `Gemfile.lock` indicates
this is a Rails 2 application.

Default process types:

```
web: bundle exec ruby script/server -p $PORT
worker: bundle exec rake jobs:work
rake: bundle exec rake
console: bundle exec script/console
```

### Rails 3

Presence of the `rails` gem with a version >= 3.0.0 and < 4.0.0 in `Gemfile.lock` indicates
this is a Rails 3 application.

Default process types:

```
web: bundle exec rails server -p $PORT
worker: bundle exec rake jobs:work
rake: bundle exec rake
console: bundle exec rails console
```

### Rails 4

Presence of the `rails` gem with a version >= 4.0.0 and < 5.0.0 in `Gemfile.lock` indicates
this is a Rails 4 application.

Default process types:

```
web: bin/rails server -p $PORT -e $RAILS_ENV
worker: bundle exec rake jobs:work
rake: bundle exec rake
console: bin/rails console
```

## Assets

If your application defines the `assets:precompile` Rake task, then it will be run
as the last step of compiling your application.

If your application is detected as Rails 3 or 4 and does not have the `rails_12factor`
gem in the `Gemfile`, the `rails3_serve_static_assets` gem will be installed into your
application which will set `config.serve_static_assets = true`. This is necessary so that
your application will serve assets out of the `public` directory, as these will not be
served by any other means.

## Run Jobs

If you need to run Rake tasks or start a Rails console inside your app, use `flynn run`. For example:

```
$ flynn run rake db:migrate
$ flynn run rails console
```

*See [our command line docs](/docs/cli#run) for more information on the `flynn run` command*
