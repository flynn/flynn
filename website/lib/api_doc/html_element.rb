module APIDoc
  class HTMLElement
    attr_accessor :name, :content, :attributes

    def initialize(name, content = '', attributes = {})
      @name, @content, @attributes = name, content, attributes
    end

    def <<(content)
      self.content << content.to_s
    end

    def to_html
      if attributes.keys.any?
        attrs = ' ' + attributes.map {
          |k,v| %(#{k}="#{v}")
        }.join(' ')
      else
        attrs = ''
      end

      %(<#{name}#{attrs}>\n#{content}\n</#{name}>\n)
    end

    def to_s
      to_html
    end
  end
end
