require 'api_doc/html_element'

module APIDoc
  class SchemaTable
    attr_reader :schema

    def initialize(schema)
      @schema = schema
    end

    def to_html
      table = el('table')
      table << table_caption.to_html
      table << table_header.to_html
      table << table_body.to_html

      table.to_html
    end

    private

    def schema_path
      URI(schema['id']).path
    end

    def table_caption
      caption = el('caption')
      caption << el('a', schema['id'], {href: schema_path})

      caption
    end

    def table_header
      thead = el('thead')
      thead << %w[ Property Type Description ].inject(el('tr')) { |row, header|
        row << el('th', header).to_html
        row
      }.to_html

      thead
    end

    def table_body
      tbody = el('tbody')

      schema['properties'].to_a.map do |name, attrs|
        tbody << property_rows(name, attrs)
      end

      tbody
    end

    def property_rows(name, attrs)
      type = []

      if attrs['format']
        type << "#{attrs['format']}"
      end

      type << attrs['type']

      if attrs['pattern']
        type << 'matching'
        type << el('code', attrs['pattern'])
      end

      if attrs['type'] == 'array' && attrs['items']
        type << 'of'
        if attrs['items']['format']
          type << attrs['items']['format']
        end
        type << "#{attrs['items']['type']}s"
      end

      type = type.join(' ')

      main_row = el('tr')
      main_row << el('td', el('code', name))
      main_row << el('td', type)
      main_row << el('td', attrs['description'].to_s.gsub(/`([^`]+?)`/, '<code>\1</code>'))

      rows = [main_row]

      if attrs['items'] && attrs['items']['type'] == 'object' && attrs['items']['properties']
        attrs['items']['properties'].each do |k, v|
          rows << property_rows("#{name}[].#{k}", v)
        end
      elsif attrs['properties']
        attrs['properties'].each do |k,v|
          rows << property_rows("#{name}.#{k}", v)
        end
      end

      rows.join
    end

    def capitalize(str)
      str[0].upcase + str[1..-1]
    end

    def el(name, content = '', attributes = {})
      HTMLElement.new(name, content, attributes)
    end
  end
end
