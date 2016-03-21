# ansi_up.js

__ansi_up__ is a simple library for converting text that contains [ANSI color escape codes](http://en.wikipedia.org/wiki/ANSI_escape_code#Colors) into equivalent HTML spans.
At the same, it also properly escapes HTML unsafe characters (&,<,>,etc.) into their proper HTML representation. It can also transform any text that looks like a URL into an HTML anchor tag.

This is compliant with AMD (require.js). This code has been used in production since early 2012. This project is actively maintained and welcomes all feedback. Thanks for reading.

Turn this terminal output:

    ESC[1;Foreground
    [1;30m 30  [1;30m 30  [1;30m 30  [1;30m 30  [1;30m 30  [1;30m 30  [1;30m 30  [1;30m 30  [0m
    [1;31m 31  [1;31m 31  [1;31m 31  [1;31m 31  [1;31m 31  [1;31m 31  [1;31m 31  [1;31m 31  [0m
    [1;32m 32  [1;32m 32  [1;32m 32  [1;32m 32  [1;32m 32  [1;32m 32  [1;32m 32  [1;32m 32  [0m
    ...

Into this browser output:

![](https://raw.github.com/drudru/ansi_up/master/sample.png)

## Browser Example

```HTML
    <script src="ansi_up.js" type="text/javascript"></script>
    <script type="text/javascript">

    var txt  = "\n\n\033[1;33;40m 33;40  \033[1;33;41m 33;41  \033[1;33;42m 33;42  \033[1;33;43m 33;43  \033[1;33;44m 33;44  \033[1;33;45m 33;45  \033[1;33;46m 33;46  \033[1m\033[0\n\n\033[1;33;42m >> Tests OK\n\n"

    var html = ansi_up.ansi_to_html(txt);

    var cdiv = document.getElementById("console");

    cdiv.innerHTML = html;

    </script>
```

## Node Example

```JavaScript
    var ansi_up = require('ansi_up');

    var txt  = "\n\n\033[1;33;40m 33;40  \033[1;33;41m 33;41  \033[1;33;42m 33;42  \033[1;33;43m 33;43  \033[1;33;44m 33;44  \033[1;33;45m 33;45  \033[1;33;46m 33;46  \033[1m\033[0\n\n\033[1;33;42m >> Tests OK\n\n"

    var html = ansi_up.ansi_to_html(txt);
```

There are examples in the repo that demonstrate an AMD/require.js/ jQuery example as well as a simple browser example.

## Installation

    $ npm install ansi_up

## API

_ansi_up_ should be called via the functions defined on the module. It is recommended that the HTML is rendered with a monospace font and black background. See the examples, for a basic theme as a CSS definition.

#### ansi_to_html (txt, options)

This replaces ANSI terminal escape codes with SPAN tags that wrap the content. See the example output above.

This function only interprets ANSI SGR (Select Graphic Rendition) codes that can be represented in HTML. For example, cursor movement codes are ignored and hidden from output.

The default style uses colors that are very close to the prescribed standard. The standard assumes that the text will have a black background. These colors are set as inline styles on the SPAN tags. Another option is to set 'use_classes: true' in the options argument. This will instead set classes on the spans so the colors can be set via CSS. The class names used are of the format ````ansi-*-fg/bg```` and ````ansi-bright-*-fg/bg```` where * is the colour name, i.e black/red/green/yellow/blue/magenta/cyan/white. See the examples directory for a complete CSS theme for these classes.

#### escape_for_html (txt)

This does the minimum escaping of text to make it compliant with HTML. In particular, the '&','<', and '>' characters are escaped. This should be run prior to ansi_to_html.

#### linkify (txt)

This replaces any links in the text with anchor tags that display the link. The links should have at least one whitespace character surrounding it. Also, you should apply this after you have run ansi_to_html on the text.

## Building

This just uses 'make'. The result is just one file. Feel free to include the file in your asset minification process.

## Running tests

To run the tests for _ansi_up_, run `npm install` to install dependencies and then:

    $ make test

## Credits

This code was developed by Dru Nelson (<https://github.com/drudru>).

Thanks goes to the following contributors for their patches:

- AIZAWA Hina (<https://github.com/fetus-hina>)
- James R. White (<https://github.com/jamesrwhite>)
- Aaron Stone (<https://github.com/sodabrew>)
- Maximilian Antoni (<https://github.com/mantoni>)
- Jim Bauwens (<https://github.com/jimbauwens>)
- Jacek Jędrzejewski (<https://github.com/eXtreme>)

## License

(The MIT License)

Copyright (c) 2011 Dru Nelson

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
'Software'), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED 'AS IS', WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WIT
