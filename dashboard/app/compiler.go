package main

import (
	"bytes"
	"html/template"
	"io"
	"log"
	"os"

	matrix "github.com/jvatic/asset-matrix-go"
)

func main() {
	m := matrix.New(&matrix.Config{
		Paths: []*matrix.AssetRoot{
			{
				GitRepo:   "git://github.com/jvatic/marbles-js.git",
				GitBranch: "master",
				GitRef:    "0a32d09dc73f87482fb12ce963c9385fabb0d036",
				Path:      "src",
			},
			{
				GitRepo:   "git://github.com/flynn/flynn-dashboard-web-icons.git",
				GitBranch: "master",
				GitRef:    "19649ac60d7da571595d54c6368fe1601bb0b79b",
				Path:      "assets",
			},
			{
				Path: "./lib/javascripts",
			},
			{
				Path: "./lib/stylesheets",
			},
			{
				Path: "./lib/images",
			},
			{
				Path: "./vendor/javascripts",
			},
			{
				Path: "./vendor/stylesheets",
			},
			{
				Path: "./vendor/fonts",
			},
		},
		Outputs: []string{
			"dashboard.js",
			"dashboard-*.js",
			"dashboard.scss",
			"ansiparse.js",
			"moment.js",
			"es6promise.js",
			"react.js",
			"react.dev.js",
			"*.png",
			"*.eot",
			"*.svg",
			"*.ttf",
			"*.woff",
		},
		OutputDir:      "./build/assets",
		AssetURLPrefix: "/assets/",
	})
	if err := m.Build(); err != nil {
		log.Fatal(err)
	}
	if err := compileTemplate(m.Manifest); err != nil {
		log.Fatal(err)
	}
	m.RemoveOldAssets()
}

func compileTemplate(manifest *matrix.Manifest) error {
	type TemplateData struct {
		Development bool
	}
	tmplHTML, err := readTemplate()
	if err != nil {
		return err
	}
	tmpl, err := template.New("dashboard.html").Funcs(template.FuncMap{
		"assetPath": func(p string) string {
			return "/assets/" + manifest.Assets[p]
		},
	}).Parse(tmplHTML)
	if err != nil {
		return err
	}
	file, err := os.Create("./build/dashboard.html")
	if err != nil {
		return err
	}
	defer file.Close()
	return tmpl.Execute(file, &TemplateData{
		Development: os.Getenv("ENVIRONMENT") == "development",
	})
}

func readTemplate() (string, error) {
	var buf bytes.Buffer
	file, err := os.Open("./lib/dashboard.html.tmpl")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(&buf, file); err != nil {
		return "", err
	}
	return buf.String(), nil
}
