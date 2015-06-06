require 'static-sprockets'
require 'es6-module-mapper'
require 'react-jsx-sprockets'
require 'marbles-js'
require 'flynn-dashboard-web-icons'
require 'yajl'
require 'dashboard/installer_import'

StaticSprockets.sprockets_config do |environment|
  MarblesJS::Sprockets.setup(environment)
  FlynnDashboardWebIcons::Sprockets.setup(environment)
  environment.append_path(File.join(Dashboard::InstallerImport::BASE_PATH, 'images'))
end

LIB_PATH = File.expand_path(File.join(File.dirname(__FILE__), 'lib'))
INSTALLER_PATH = File.join(LIB_PATH, 'installer')
VENDOR_PATH = File.expand_path(File.join(File.dirname(__FILE__), 'vendor'))

StaticSprockets.configure(
  :asset_roots => [
    LIB_PATH,
    INSTALLER_PATH,
    VENDOR_PATH
  ],
  :asset_types => %w( javascripts stylesheets fonts images ),
  :output_asset_names => %w( dashboard.css dashboard.js react.js react.dev.js moment.js es6promise.js ).concat(
    Dir[File.join(LIB_PATH, 'images', '*')].map { |path| File.basename(path) }
  ).concat(
    Dir[File.join(INSTALLER_PATH, 'images', '*')].map { |path| File.basename(path) }
  ).concat(
    Dir[File.join(VENDOR_PATH, 'fonts', '*')].map { |path| File.basename(path) }
  ),
  :layout => File.expand_path(File.join(File.dirname(__FILE__), 'lib', 'dashboard.html.erb')),
  :layout_output_name => ENV['LAYOUT_OUTPUT_FILENAME'] || 'dashboard.html',
  :output_dir => ENV['OUTPUT_DIR'] || File.expand_path(File.join(File.dirname(__FILE__), 'build')),
  :asset_root => ENV['ASSET_ROOT'] || '/assets',
  :asset_cache_dir => ENV['ASSET_CACHE_DIR']
)
