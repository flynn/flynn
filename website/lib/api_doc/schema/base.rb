module APIDoc
  module Schema
    class BaseSchema
      def self.parse_schema(schema, parent=nil)
        if schema.is_a?(BaseSchema)
          return schema
        end

        if schema.has_key?("$ref")
          return RefSchema.new(schema["$ref"], parent)
        end

        if schema.has_key?("allOf")
          return AllOfSchema.new(schema, parent)
        end

        case schema['type']
        when 'object'
          ObjectSchema.new(schema, parent)
        when 'array'
          ArraySchema.new(schema, parent)
        when 'string'
          case schema['format']
          when 'uuid'
            UUIDSchema.new(schema, parent)
          when 'date-time'
            DateTimeSchema.new(schema, parent)
          when 'uri'
            URISchema.new(schema, parent)
          else
            StringSchema.new(schema, parent)
          end
        when 'boolean'
          BooleanSchema.new(schema, parent)
        when 'number'
          NumberSchema.new(schema, parent)
        else
          BaseSchema.new(schema, parent)
        end
      end

      attr_reader :id

      def initialize(schema, parent)
        @schema = schema
        @parent = parent
        @id = @schema['id']

        if schema.has_key?('definitions')
          schema['definitions'].each_pair do |k,v|
            if !v.is_a?(BaseSchema)
              schema['definitions'][k] = BaseSchema.parse_schema(v, self)
            end
          end
        end
      end

      def [](k)
        @schema[k]
      end

      def expand_refs!
        if @schema.has_key?('definitions')
          @schema['definitions'].each_pair do |k,v|
            if v.is_a?(RefSchema)
              @schema['definitions'][k] = Schema.find(v.id)
            else
              v.expand_refs!
            end
          end
        end
      end

      def to_hash
        hash = {}
        @schema.each_pair do |k,v|
          case k
          when 'definitions'
            definitions = {}
            v.each_pair do |dk, dv|
              if dv.is_a?(BaseSchema)
                definitions[dk] = dv.to_hash
              else
                definitions[dk] = dv
              end
            end
            hash[k] = definitions
          else
            hash[k] = v
          end
        end
        hash
      end

      def to_markdown
        markdown = []
        if @schema['title']
          markdown << "# #{@schema['title']}"
        end
        if @schema['description']
          markdown << @schema['description']
        end
        markdown << to_html_table
        markdown.join("\n\n")
      end

      def to_html_table
        SchemaTable.new(self).to_html
      end
    end
  end
end
