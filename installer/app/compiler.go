package main

import (
	"log"

	matrix "github.com/jvatic/asset-matrix-go"
)

func main() {
	m := matrix.New(&matrix.Config{
		Paths: []*matrix.AssetRoot{
			{
				GitRepo:   "git://github.com/jvatic/marbles-js.git",
				GitBranch: "master",
				GitRef:    "50fe2ed6d530d9b695b98a775dcc28ec7c9478bc",
				Path:      "src",
			},
			{
				Path: "./src",
			},
			{
				Path: "./vendor",
			},
		},
		Outputs: []string{
			"normalize.css",
			"font-awesome.scss",
			"application.css",
			"application.js",
			"react.js",
			"*.png",
			"*.gif",
			"*.eot",
			"*.svg",
			"*.ttf",
			"*.woff",
		},
		OutputDir:      "./build",
		AssetURLPrefix: "/assets/",
	})
	if err := m.Build(); err != nil {
		log.Fatal(err)
	}
	m.RemoveOldAssets()
}
