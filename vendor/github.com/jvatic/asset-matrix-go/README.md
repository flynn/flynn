asset-matrix-go
===============

Asset compilation with flexible external dependency management.

This is very much just hacked together, but it works and I plan on making it a lot better when I have the time.

Supports SCSS (`.scss`), ERB (`.erb`), JSX (`.jsx`), and ES6 modules for JavaScript (`.js`).

There is asset caching between builds for everything except SCSS and ERB (as these have access to the cache breaker and significant refactoring is required to handle this properly).

## Usage

```go
// compiler.go
package main

import (
	"log"

	matrix "github.com/jvatic/asset-matrix-go"
)

func main() {
	m := matrix.New(&matrix.Config{
		Paths: []*matrix.AssetRoot{
			{
				Path: "./src",
			},
			{
				GitRepo:   "git://github.com/jvatic/marbles-js.git",
				GitBranch: "master",
				GitRef:    "6d5491bbc51f4454e0c605af6e7faa4e0539441a",
				Path:      "src",
			},
		},
		Outputs: []string{
			"application.js",
			"application.scss",
			"marbles/*.js",
			"*.png",
		},
		OutputDir: "./build",
	})
	if err := m.Build(); err != nil {
		log.Fatal(err)
	}
	m.RemoveOldAssets()
}
```

```sh
go run compiler.go
```

See [here](https://github.com/flynn/flynn/blob/007ec3ced7b4323153b58e88b2709d3290070eb1/dashboard/app/compiler.go#L82-L119) for an example of compiling an HTML template.

`Outputs: []string{}` will output everything, or you can explicitly specify what you'd like in your build directory.

## How import paths are resolved

Lets say you have two`AssetRoot`s specified in your config pointing to `./src` and `./vendor`. If `./vendor` has the following relative paths, `foo/bar.js` and `foo/bar.scss`, then you can import them as `import Bar from 'foo/bar';` and `@import "foo/bar";` in your JavaScript and SCSS files respectively. However, if you have either of these paths in `./src` (which we're assuming is given before `./vendor` in the config), then that file will be used instead of the one in `./vendor`.

## Contributing

Pull requests are always welcome!

Though it's always a good idea to discuss things in an [issue](https://github.com/jvatic/asset-matrix-go/issues) before implementing them.
