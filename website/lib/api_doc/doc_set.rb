require 'yajl'

module APIDoc
  class DocSet
    def self.compile(name, id, output_path, exclude = [])
      markdown = new(name, id, exclude).to_markdown
      File.open(output_path, 'w') do |file|
        file.write(markdown)
      end
    end

    def initialize(name, id, exclude = [])
      @schemas = Schema.find_all(id).reject { |s| exclude.include?(s.id) }
      @name = name
      @id = id
      @examples = {}
      load_examples!
    end

    def to_markdown
      @schemas.map do |schema|
        [ schema.to_markdown,
          schema['examples'].to_a.map { |item|
            name = item['name']
            example = @examples[name]
            example ? example.to_markdown : "```\nexample #{name} not found\n```"
          }.join("\n\n")
        ].join("\n\n")
      end.join("\n\n")
    end

    private

    def load_examples!
      path = File.join(PROJECT_ROOT, 'examples', "#{@name}.json")
      Yajl::Parser.parse(File.read(path)).each_pair do |k,v|
        @examples[k] = Example.new(v)
      end
    end

    class Example
      attr_reader :data

      def initialize(data)
        @data = data
      end

      def to_markdown
        markdown = []

        request = data['request']
        response = data['response']

        # request headers
        markdown << "```"
        markdown << [request['method'], request['url'], 'HTTP/1.1'].join(' ')
        markdown << pretty_headers(request['headers'])
        markdown << "```"

        # request body
        if request.has_key?('body')
          markdown << markdown_body(request['body'], request['headers']['Content-Type'])
          markdown << ''
        end

        # response headers
        markdown << "```"
        markdown << pretty_headers(response['headers'])
        markdown << "```"

        # response body
        markdown << markdown_body(response['body'], response['headers']['Content-Type'])
        markdown << ''

        markdown.join("\n")
      end

      private

      def pretty_headers(headers)
        headers.inject([]) { |res, (name, value)|
          res << "#{name}: #{value}"
          res
        }.join("\n")
      end

      def markdown_body(body, content_type)
        markdown = []
        body = body.to_s
        if content_type =~ /\bjson/i
          markdown << "```json"
          markdown << Yajl::Encoder.encode(Yajl::Parser.parse(body), :pretty => true, :indent => '  ')
        else
          markdown << "```"
          markdown << body
        end
        markdown << "```"
        markdown.join("\n")
      end
    end
  end
end
