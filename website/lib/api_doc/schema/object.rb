require 'api_doc/schema/base'

module APIDoc
  module Schema
    class ObjectSchema < BaseSchema
      def initialize(schema, parent)
        super
        @properties = schema['properties'] || {}
        @properties.each_pair do |k,v|
          if !v.has_key?('id')
            id = URI(@id)
            id.fragment = File.join(id.fragment, k)
            v['id'] = id.to_s
          end
          @properties[k] = BaseSchema.parse_schema(v, self)
        end
      end

      def expand_refs!
        @properties.each_pair do |k,v|
          if v.is_a?(RefSchema)
            @properties[k] = Schema.find(v.id)
          else
            v.expand_refs!
          end
        end
      end

      def to_hash
        hash = super
        @schema.each_pair do |k,v|
          case k
          when 'properties'
            properties = {}
            v.each_pair do |pk, pv|
              properties[pk] = pv.to_hash
            end
            hash[k] = properties
          else
            hash[k] = v
          end
        end
        hash
      end
    end
  end
end
