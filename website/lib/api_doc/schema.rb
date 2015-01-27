require 'yajl'
require 'uri'
require 'json-pointer'

require 'api_doc/schema/base'
require 'api_doc/schema/ref'
require 'api_doc/schema/all_of'
require 'api_doc/schema/object'
require 'api_doc/schema/array'
require 'api_doc/schema/string'
require 'api_doc/schema/uuid'
require 'api_doc/schema/date_time'
require 'api_doc/schema/uri'
require 'api_doc/schema/boolean'
require 'api_doc/schema/number'

require 'api_doc/schema_table'

module APIDoc
  module Schema
    def self.load_all(paths)
      paths.map do |path|
        load(path)
      end
    end

    def self.load(path)
      data = Yajl::Parser.parse(File.read(path))
      schema = BaseSchema.parse_schema(data)
      schemas[schema.id] = schema
      schema
    rescue => e
      puts "Error loading #{path}"
      raise e
    end

    def self.find(id)
      id = URI(id.to_s)
      fragment = id.fragment
      id.fragment = ""
      parent = schemas[id.to_s]
      return unless parent
      return parent if fragment.nil? || fragment == ''
      pointer = JsonPointer.new(parent.to_hash, fragment)
      schema = pointer.value
      return unless schema
      unless schema.is_a?(BaseSchema)
        schema = BaseSchema.parse_schema(schema, parent)
        pointer.value = schema
      end
      if schema.is_a?(RefSchema)
        schema = find(schema.id)
        pointer.value = schema
      end
      schema.expand_refs!
      schema
    end

    def self.find_all(parent_id)
      @schemas.keys.keep_if do |schema_id|
        schema_id.to_s[0..parent_id.length-1] == parent_id
      end.map do |schema_id|
        @schemas[schema_id]
      end
    end

    def self.schemas
      @schemas ||= {}
    end
  end
end
