require 'api_doc/schema/base'

module APIDoc
  module Schema
    class AllOfSchema < BaseSchema
      def initialize(schema, parent)
        super
        schema['allOf'].map! do |s|
          if !s.has_key?(id)
            s['id'] = @id
          end
          BaseSchema.parse_schema(s, self)
        end
      end

      def expand_refs!
        @schema['allOf'].map! do |s|
          if s.is_a?(RefSchema)
            Schema.find(s.id)
          else
            s.expand_refs!
            s
          end
        end
      end

      def to_hash
        hash = super
        @schema.each_pair do |k,v|
          case k
          when 'allOf'
            hash[k] = v.map { |s| s.to_hash }
          else
            if v.is_a?(BaseSchema)
              hash[k] = v.to_hash
            else
              hash[k] = v
            end
          end
        end
        hash
      end
    end
  end
end
