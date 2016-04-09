require 'flynn-docs/version'

module FlynnDocs
  def self.dir
    @dir ||= File.expand_path(File.join(__FILE__, '..', '..'))
  end

  def self.content_dir
    @content_dir ||= File.join(dir, 'content')
  end

  def self.images_dir
    File.join(dir, 'images')
  end

  def self.docs_nav_path
    File.join(dir, 'docs-nav.json')
  end

  def self.redirects_path
    File.join(dir, 'redirects.json')
  end

  def self.contributing_markdown
    File.read(File.join(dir, '..', 'CONTRIBUTING.md'))
  end

  def self.api_examples(name)
    path = File.join(dir, 'api-examples', "#{name}.json")
    Yajl::Parser.parse(File.read(path))
  end

  def self.api_examples_preface(name)
    path = File.join(dir, 'api-examples', "#{name}.md")
    File.exists?(path) ? File.read(path) : ''
  end

  def self.manifest
    @manifest ||= Dir[File.join(content_dir, '**', '*')].reject do |path|
      File.directory?(path)
    end.map do |path|
      {
        :path => File.expand_path(path),
        :logical_path => path.sub(content_dir + '/', '')
      }
    end
  end
end
