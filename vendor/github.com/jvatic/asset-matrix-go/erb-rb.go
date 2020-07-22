package assetmatrix

var erbRB = `
require 'erb'

class TemplateContext
  AssetNotFoundError = Class.new(StandardError)

  def initialize
  end

  def asset_path(name)
    *n, ext = name.split('.')
    n.join('.') + '-' + ENV['CACHE_BREAKER'] + '.' + ext
  end
end

def template_binding
  TemplateContext.new().instance_eval { binding }
end

def main
  t = ::ERB.new(STDIN.read, nil, '<>')
  print(t.result(template_binding))
end
main()
`
