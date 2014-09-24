# -*- encoding: utf-8 -*-
lib = File.expand_path('../dashboard/app/lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)
require 'dashboard/version'

Gem::Specification.new do |gem|
  gem.name          = "flynn-dashboard"
  gem.version       = Dashboard::VERSION
  gem.authors       = ["Jesse Stuart"]
  gem.email         = ["jesse@jessestuart.ca"]
  gem.description   = %q{Flynn dashboard assets}
  gem.summary       = %q{Flynn dashboard assets}
  gem.homepage      = ""

  gem.files         = `git ls-files dashboard/app`.split($/)
  gem.executables   = gem.files.grep(%r{^bin/}).map{ |f| File.basename(f) }
  gem.test_files    = gem.files.grep(%r{^(test|spec|features)/})
  gem.require_paths = ["dashboard/app/lib"]
end
