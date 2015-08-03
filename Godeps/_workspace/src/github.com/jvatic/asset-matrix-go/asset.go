package assetmatrix

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Asset interface {
	Open() (*os.File, error)
	Initialize() error
	Path() string
	RelPath() (string, error)
	SetIndexKey(string)
	IndexKey() string
	ImportPaths() []string
	Compile() (io.Reader, error)
	OutputExt() string
	OutputPath() string
}

func NewAsset(r *AssetRoot, p string) Asset {
	var a Asset
	a = &GenericAsset{
		r: r,
		p: p,
	}
	exts := strings.Split(filepath.Base(p), ".")[1:]
	for i := len(exts) - 1; i >= 0; i-- {
		ext := exts[i]
		ap := strings.TrimSuffix(p, "."+strings.Join(exts[i:], ".")) + "." + ext
		switch ext {
		case "js":
			a = NewJavaScriptAsset(r, a, ap)
		case "jsx":
			a = NewJSXAsset(r, a, ap)
		case "erb":
			a = NewERBAsset(r, a, ap)
		case "scss":
			a = NewSCSSAsset(r, a, ap)
		}
	}
	return a
}

func NewJavaScriptAsset(r *AssetRoot, input Asset, p string) Asset {
	return &JavaScriptAsset{
		input:             input,
		r:                 r,
		p:                 p,
		transformerJSPath: r.transformerJSPath,
	}
}

func NewJSXAsset(r *AssetRoot, input Asset, p string) Asset {
	return &JSXAsset{
		input: input,
		r:     r,
		p:     p,
	}
}

func NewERBAsset(r *AssetRoot, input Asset, p string) Asset {
	return &ERBAsset{
		input:        input,
		r:            r,
		p:            p,
		cacheBreaker: r.cacheBreaker,
		erbRBPath:    r.erbRBPath,
	}
}

func NewSCSSAsset(r *AssetRoot, input Asset, p string) Asset {
	return &SCSSAsset{
		input:          input,
		r:              r,
		p:              p,
		findAsset:      r.findAsset,
		scssJSPath:     r.scssJSPath,
		assetURLPrefix: r.assetURLPrefix,
	}
}
