lib = File.expand_path('../lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)

# Reload the browser automatically whenever files change
begin
  require 'middleman-livereload'
  activate :livereload
rescue LoadError
end

helpers do
  def active_nav_class(path, opts={})
    current = current_path.sub(/\.html\Z/, '').sub(/\/index\Z/, '')
    path = full_path(path).sub(/\A\//, '').sub(/\.html\Z/, '').sub(/\/index\Z/, '')

    if opts[:not] && opts[:not].match(current)
      return ""
    end

    r = Regexp.new("\\A#{Regexp.escape(path)}")
    r.match(current) ? "active" : ""
  end

  def nav_link_with_active(text, target, attributes = {})
    target_path = full_path(target).sub(/\A\//, '').sub(/\.html\Z/, '')
    item_path = current_path.sub(/\.html\Z/, '')

    active = if attributes.delete(:top)
               /\A#{target_path}/ =~ item_path
             else
               target_path == item_path
             end

    "<li #{'class="active"' if active}>" + link_to(text, target, attributes) + "</li>"
  end
end

require 'builder'
require 'markdown_html'

set :markdown_engine, :redcarpet
set :markdown, REDCARPET_EXTENTIONS

activate :blog do |blog|
  blog.permalink = "blog/:title.html"
  blog.sources = "blog/:title.html"
  blog.layout = 'article'
end

set :css_dir, 'stylesheets'

set :js_dir, 'javascripts'

set :images_dir, 'images'

# React JSX compiler
require 'react-jsx-sprockets'

# Add marbles-js to sprockets paths
require 'marbles-js'
MarblesJS::Sprockets.setup(sprockets)

require 'fly'
Fly::Sprockets.setup(sprockets)

require 'cupcake-icons'
CupcakeIcons::Sprockets.setup(sprockets)

# Build-specific configuration
configure :build do
  activate :minify_css
  activate :minify_javascript
  activate :asset_hash
  activate :gzip
end

# Do the equivalent of nginx "try_files $uri $uri.html" in development
class HTMLRedirect < Struct.new(:app)
  def call(env)
    status, headers, body = app.call(env)

    if status == 404 && env["PATH_INFO"] !~ /.html\Z/
      return redirect("#{env["PATH_INFO"]}.html")
    end

    [status, headers, body]
  end

  def redirect(path)
    [
      301,
      { "Location" => path },
      [%{You are being redirected to <a href="#{path}">#{path}</a>}]
    ]
  end
end

use HTMLRedirect
