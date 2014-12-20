require 'static-sprockets'
require 'react-jsx-sprockets'
require 'marbles-js'
require 'flynn-dashboard-web-icons'
require 'yajl'

StaticSprockets.sprockets_config do |environment|
  MarblesJS::Sprockets.setup(environment)
  FlynnDashboardWebIcons::Sprockets.setup(environment)
end

StaticSprockets.configure(
  :asset_roots => [
    File.expand_path(File.join(File.dirname(__FILE__), 'lib')),
    File.expand_path(File.join(File.dirname(__FILE__), 'vendor'))
  ],
  :asset_types => %w( javascripts stylesheets fonts images ),
  :layout => File.expand_path(File.join(File.dirname(__FILE__), 'lib', 'dashboard.html.erb')),
  :layout_output_name => ENV['LAYOUT_OUTPUT_FILENAME'] || 'dashboard.html',
  :output_dir => ENV['OUTPUT_DIR'] || File.expand_path(File.join(File.dirname(__FILE__), 'build')),
  :asset_root => ENV['ASSET_ROOT'] || '/assets',
  :asset_cache_dir => ENV['ASSET_CACHE_DIR']
)
