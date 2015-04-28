require 'bundler/setup'
require 'sprockets'
require 'es6-module-mapper'
require 'react-jsx-sprockets'
require 'marbles-js'
require 'fileutils'

module Installer
  def self.input_dir
    @input_dir ||= File.join(File.dirname(__FILE__), 'src')
  end

  def self.vendor_dir
    @vendor_dir ||= File.join(File.dirname(__FILE__), 'vendor')
  end

  def self.output_dir
    @output_dir ||= File.join(File.dirname(__FILE__), 'build')
  end

  def self.sprockets_environment
    if @sprockets_environment
      return @sprockets_environment
    end

    # Setup Sprockets Environment
    @sprockets_environment = ::Sprockets::Environment.new do |env|
      env.logger = Logger.new(STDOUT)
      env.context_class.class_eval do
        include MarblesJS::Sprockets::Helpers
      end

      # we're not using the directive processor, so unregister it
      env.unregister_preprocessor(
        'application/javascript', ::Sprockets::DirectiveProcessor)
    end
    MarblesJS::Sprockets.setup(@sprockets_environment)
    @sprockets_environment.append_path(input_dir)
    @sprockets_environment.append_path(vendor_dir)

    return @sprockets_environment
  end

  def self.compile
    manifest = ::Sprockets::Manifest.new(
      sprockets_environment,
      output_dir,
      File.join(output_dir, 'manifest.json')
    )

    asset_names = %w[application.js application.css react.js]
    Dir[File.join(input_dir, 'images', '*')].each do |path|
      asset_names.push(File.join('images', File.basename(path)))
    end
    Dir[File.join(vendor_dir, 'fonts', '*')].each do |path|
      asset_names.push(File.join('fonts', File.basename(path)))
    end

    manifest.compile(asset_names)
  end
end
