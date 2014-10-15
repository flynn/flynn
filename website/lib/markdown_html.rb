require 'pygments.rb'
require 'cgi'
require 'middleman-core/renderers/redcarpet'

REDCARPET_EXTENTIONS = {
  fenced_code_blocks: true,
  no_intra_emphasis: true,
  autolink: true,
  tables: true,
  strikethrough: true,
  lax_html_blocks: true,
  space_after_headers: true,
  superscript: true
}.freeze

module MarkdownHelpers
  def anchor(text, level)
    if m = text.match(/\A<a[^>]+>([^<]+)<\/a>/)
      text = m[1]
    end

    @parents ||= {}
    parent = @parents[level]
    text = (parent ? "#{parent}-" : "") + text.downcase.strip.gsub(/[^a-z0-9 ]/, '').gsub(/\s+/, '-')
    @parents[level+1] = text
    text
  end

  def el(el, content, attributes = {})
    if content
      attrs = attributes ? ' ' + attributes.map { |k,v| "#{k}=\"#{v}\"" }.join(' ') : ''
      "<#{el}#{attrs}>\n#{content}</#{el}>\n"
    else
      ''
    end
  end
end

class Middleman::Renderers::MiddlemanRedcarpetHTML
  include Redcarpet::Render::SmartyPants
  include MarkdownHelpers

  def block_code(code, language)
    language == "text" ? el('pre', CGI::escapeHTML(code)) : Pygments.highlight(code, lexer: language)
  end

  def header(text, level)
    el("h#{level}", text, id: anchor(text, level))
  end
end

class MarkdownHTMLTOC < Redcarpet::Render::Base
  include MarkdownHelpers

  def header(text, level)
    return "" if text =~ /\Alayout:/

    if m = text.match(/\A\[([^\]]+)\]\(([^\)]+)\)/)
      text = m[1]
    end

    @current_level ||= 0

    html = []

    if level > @current_level
      @current_level.upto(level-1) { |i|
        html << '<ul>'
        html << '<li>'
      }
    elsif level < @current_level
      level.upto(@current_level-1) { |i|
        html << '</li>'
        html << '</ul>'
      }
    else
      html << '</li>'
      html << '<li>'
    end

    html << el('a', text, href: '#'+ anchor(text, level))

    @current_level = level

    html.join("\n")
  end

  def doc_footer
    html = []
    @current_level ||= 2
    (@current_level-1).times {
      html << "</li>"
      html << "</ul>"
    }
    html.join("\n")
  end
end
