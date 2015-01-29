require 'yajl'
require 'active_support/core_ext/string/strip'

module APIDoc
  class DocSet
    ExampleNotFoundError = Class.new(StandardError)

    def self.compile(name, id, output_path, exclude = [])
      markdown = new(name, id, exclude).to_markdown
      File.open(output_path, 'w') do |file|
        file.write(markdown)
      end
    end

    def initialize(name, id, exclude = [])
      @schemas = Schema.find_all(id).reject { |s| exclude.include?(s.id) }.sort_by { |s| s['sortIndex'] }
      @name = name
      @id = id
      @examples = {}
      load_examples!
    end

    def frontmatter
      <<-STR.strip_heredoc
      ---
      title: #{capitalize(@name)}
      layout: docs
      ---
      STR
    end

    def to_markdown
      frontmatter + @schemas.map do |schema|
        [ schema.to_markdown,
          schema['examples'].to_a.map { |id|
            id = "https://flynn.io/"+ id
            example = find_example(id)
            if example.nil?
              raise ExampleNotFoundError.new("example #{id} not found")
            end
            example.to_markdown
          }
        ]
      end.flatten.join("\n\n")
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
        request = data['request']
        response = data['response']

<<-STR
<article class="example clearfix">
  <header>

## #{schema['title']}

  </header>
  <button class="example-toggle btn btn-primary btn-small pull-right" data-expanded="Collapse">Example</button>

  #{schema['description'] && schema['description'] != '' ? schema['description'] : '&nbsp;'}

  <section class="example-request">
    <header>
      <h4>Request</h4>
    </header>

```
#{request['method']} #{request['url']} HTTP/1.1
#{pretty_headers(request['headers'])}
```
#{request.has_key?('body') ? markdown_body(request['body'], request['headers']['Content-Type']) : ''}

  </section>

  <section class="example-response">
    <header>
      <h4>Response</h4>
    </header>

```
#{pretty_headers(response['headers'])}
```
#{markdown_body(response['body'], response['headers']['Content-Type'])}

  </section>
</article>
STR
      end

      private

      def pretty_headers(headers)
        headers.map { |name, value| "#{name}: #{value}" }.join("\n")
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
