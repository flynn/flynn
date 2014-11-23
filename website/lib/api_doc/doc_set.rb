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

    def frontmatter
      str = []
      str << %(---)
      str << %(title: #{capitalize(@name)})
      str << %(layout: docs)
      str << %(---)
      str << ''
      str.join("\n")
    end

    def to_markdown
      frontmatter + @schemas.map do |schema|
        [ schema.to_markdown,
          schema['examples'].to_a.map { |id|
            example = find_example(id)
            example ? example.to_markdown : "```\nexample #{id} not found\n```"
          }.join("\n\n")
        ].join("\n\n")
      end.join("\n\n")
    end

    private

    def capitalize(str)
      str[0].upcase + str[1..-1]
    end

    def find_example(id)
      @examples[id]
    end

    def load_examples!
      path = File.join(PROJECT_ROOT, 'examples', "#{@name}.json")
      Yajl::Parser.parse(File.read(path)).each_pair do |k,v|
        id = "https://flynn.io/schema/examples/#{@name}/#{k}#"
        @examples[id] = Example.new(id, v)
      end
    end

    class Example
      attr_reader :data, :schema

      def initialize(id, data)
        @data = data
        @schema = Schema.find(id)
      end

      def to_markdown
        markdown = []

        request = data['request']
        response = data['response']

        markdown << %(<article class="example">)
        markdown << %(<header>)
        markdown << %(## #{schema['title']})
        markdown << %(</header>)

        if schema['description']
          markdown << ''
          markdown << schema['description']
          markdown << ''
        end

        markdown << %(<section class="example-request">)
        markdown << %(<header>)
        markdown << %(<h4>Request</h4>)
        markdown << %(</header>)
        markdown << ''

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

        markdown << ''
        markdown << %(</section>)

        markdown << %(<section class="example-response">)
        markdown << %(<header>)
        markdown << %(<h4>Response</h4>)
        markdown << %(</header>)
        markdown << ''

        # response headers
        markdown << "```"
        markdown << pretty_headers(response['headers'])
        markdown << "```"

        # response body
        markdown << markdown_body(response['body'], response['headers']['Content-Type'])
        markdown << ''

        markdown << %(</section>)

        markdown << %(</article>)

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
        if content_type =~ /\bjson/i && body
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
