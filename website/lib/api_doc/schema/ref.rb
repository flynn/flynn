require 'api_doc/schema/base'

module APIDoc
  module Schema
    class RefSchema < BaseSchema
      def initialize(ref, parent)
        @id = URI(ref)
        if !@id.absolute? && parent
          parent_id = URI(parent.id)
          @id.scheme = parent_id.scheme
          @id.host = parent_id.host
          if @id.path[0] != '/'
            @id.path = '/'+ @id.path
          end
        end
        @id = @id.to_s
        @parent = parent
      end

      def to_hash
        { "$ref" => @id }
      end
    end
  end
end
