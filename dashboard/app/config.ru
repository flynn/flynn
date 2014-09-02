require 'bundler'
Bundler.require

require './config'

require 'static-sprockets/app'
map '/' do
  run StaticSprockets::App.new
end

class ConfigJSON
  def call(env)
    [502, {}, []]
  end
end

map '/config' do
  run ConfigJSON.new
end
