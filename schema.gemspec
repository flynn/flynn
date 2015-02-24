# -*- encoding: utf-8 -*-
lib = File.expand_path('../schema/lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)
require 'flynn-schema/version'

Gem::Specification.new do |gem|
  gem.name          = "flynn-schema"
  gem.version       = FlynnSchema::VERSION
  gem.authors       = ["Jesse Stuart"]
  gem.email         = ["jesse@jessestuart.ca"]
  gem.description   = %q{Flynn JSON schemas}
  gem.summary       = %q{Flynn JSON schemas}
  gem.homepage      = ""

  gem.files         = `git ls-files schema`.split($/)
  gem.executables   = gem.files.grep(%r{^bin/}).map{ |f| File.basename(f) }
  gem.test_files    = gem.files.grep(%r{^(test|spec|features)/})
  gem.require_paths = ["schema/lib"]
end
