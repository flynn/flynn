require 'api_doc/schema'
require 'api_doc/doc_set'

module APIDoc
  PROJECT_ROOT = File.expand_path(File.join(File.dirname(__FILE__), '..'))

  def self.compile(docsets = %w( controller ))
    schema_dir = File.join(PROJECT_ROOT, 'schema')

    # load all schemas and resolve refs
    schema_paths = Dir[File.join(schema_dir, "**", "*.json")]
    schemas = Schema.load_all(schema_paths)
    schemas.each { |schema| schema.expand_refs! }

    # compile schemas and examples into markdown
    docset_paths = Dir[File.join(schema_dir, "*")].keep_if do |path|
      File.directory?(path) && docsets.include?(File.split(path).last)
    end.map do |path|
      'https://flynn.io/schema/'+ File.split(path).last
    end
    docset_paths.each do |id|
      name = File.split(id).last
      output_path = File.join(PROJECT_ROOT, 'source', 'docs', name+'.md')
      DocSet.compile(name, id, output_path)
    end
  end
end
