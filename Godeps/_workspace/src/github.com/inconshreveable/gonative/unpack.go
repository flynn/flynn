package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func unpackFile(dest string, r io.Reader, fname string, mode os.FileMode) error {
	dir, name := filepath.Split(fname)
	dirPath := filepath.Join(dest, dir)
	filePath := filepath.Join(dirPath, name)
	if !strings.HasPrefix(filePath, dest) {
		return fmt.Errorf("%q is outside of %s", fname, dest)
	}
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func unpackTarGz(dest string, r *os.File) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gr)
	for {
		f, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
		if f.Typeflag != tar.TypeReg && f.Typeflag != tar.TypeReg {
			continue
		}
		if err := unpackFile(dest, tr, f.Name, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
}

func unpackZip(dest string, r *os.File) error {
	stat, err := r.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(r, stat.Size())
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		fr, err := f.Open()
		if err != nil {
			return err
		}
		err = unpackFile(dest, fr, f.Name, f.Mode())
		fr.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
