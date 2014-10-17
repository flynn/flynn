require 'tilt/erb'
require 'active_support/core_ext/string/strip'
require 'active_support/core_ext/string/output_safety'

module Processor
  extend ERB::Util

  def self.parse(text)
    sections = text.split(/\n\n(?=[[:alnum:]]+\:)/) # Split on "[/n/n]Section:"
    description = sections.shift.split("\n\n")
    usage = description.shift

    sections.map! do |section|
      _, title, content = section.partition(/[[:alnum:]]+\:/)
      { title: title.chomp(':'), content: content.strip_heredoc }
    end
    [usage, description, sections]
  end

  def self.preprocess(section)
    case section[:title]
    when /Options/
      output = [' Flag | Description', '------|--------']
      section[:content].each_line do |line|
        line = line.strip
        next if line.empty?
        f, d = line.split(/\s{2,}/)
        f = f.split(/,\s*/).map { |s| "`#{s}`" }.join(" ")
        output << "#{f} | #{d}"
      end
      output.join("\n")
    when /Commands/
      section[:content].each_line.map do |line|
        line.gsub(/^(\w+)\s{2,}/, '- **\1** ')
      end.join
    when /Examples?/
      section[:content].split("\n\n").map do |example|
        example.strip!
        if example.start_with? '$'
          "\n```\n#{example}\n```\n"
        else
          h example
        end
      end.join("\n")
    else
      h section[:content]
    end
  end
end
