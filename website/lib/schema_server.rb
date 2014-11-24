class SchemaServer
  PROJECT_ROOT = File.expand_path(File.join(File.dirname(__FILE__), '..'))

  def initialize(app)
    @app = app
  end

  def call(env)
    path = env['PATH_INFO']
    if path =~ /\A\/schema\//
      schema_path = path.sub(/\A\/schema\//, '') + '.json'
      serve_schema(schema_path) || env
    else
      env
    end
  end

  private

  def serve_schema(path)
    path = File.join(PROJECT_ROOT, 'schema', path)
    return unless File.exists?(path)
    [200, { 'Content-Type' => 'application/json' }, [File.read(path)]]
  end
end
