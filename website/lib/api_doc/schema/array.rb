require 'api_doc/schema/base'

module APIDoc
  module Schema
    class ArraySchema < BaseSchema
      def initialize(schema, parent)
        super
        if schema['items'] && !schema['items'].is_a?(BaseSchema)
          schema['items'] = BaseSchema.parse_schema(schema['items'], self.id ? self : parent)
        end
      end

      def expand_refs!
        items = @schema['items']
        if items.is_a?(RefSchema)
          @schema['items'] = Schema.find(items.id)
        elsif items.is_a?(BaseSchema)
          items.expand_refs!
        end
      end

      def to_hash
        hash = super
        if @schema['items'].is_a?(BaseSchema)
          hash['items'] = @schema['items'].to_hash
        end
        hash
      end
    end
  end
end
