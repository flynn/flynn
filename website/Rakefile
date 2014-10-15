task compile: [:cli, :copy_contributing] do
  sh 'middleman build --no-clean'
end

task :cli do
  require 'yajl'
  require_relative 'lib/cli_doc_compiler'

  json = Yajl::Parser.parse(File.read('cli.json'))
  template = Tilt::ERBTemplate.new('lib/cli.md.erb')

  File.open('source/docs/cli.html.md', 'w') do |f|
    f.write template.render(Processor, docs: json)
  end
end

task :copy_contributing do
  source = <<EOF
---
title: Contributing
layout: docs
---
EOF
  source << File.read("../CONTRIBUTING.md")
  File.write("source/docs/contributing.html.md", source)
end
