module Dashboard
  module InstallerImport
    BASE_PATH = ENV['INSTALLER_SRC_DIR'] || File.expand_path('../../installer', __FILE__)

    extend self

    def javascript(path)
      if path[0] != '/'
        path = File.join(BASE_PATH, path)
      end
      unless File.exists?(path)
        if ext = %w[ .js .js.jsx ].find { |i| File.exists?(path + i) }
          path = path + ext
        else
          throw "Asset not found: #{path}"
        end
      end
      data = File.read(path)

      data.gsub!(/^(?:import|export) .*$/, '')

      data
    end
  end
end
