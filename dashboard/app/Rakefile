require 'bundler/setup'

require './config'
require 'static-sprockets/tasks/assets'
require 'static-sprockets/tasks/layout'

task :compile => ["assets:precompile", "layout:compile"] do
end
