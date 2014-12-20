require 'dashboard/version'

module Dashboard
  def self.settings
    @settings ||= {}
  end

  def self.configure(options = {})
    unless self.settings[:asset_paths]
      self.settings[:asset_paths] = [
        File.expand_path('../javascripts', __FILE__),
        File.expand_path('../stylesheets', __FILE__),
        File.expand_path('../images', __FILE__)
      ]
    end
  end

  module Sprockets
    # Append asset paths to an existing Sprockets environment
    def self.setup(environment, options = {})
      Dashboard.configure(options)
      Dashboard.settings[:asset_paths].each do |path|
        environment.append_path(path)
      end
    end
  end
end
