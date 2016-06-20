package archiver

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	c_ISDIR = 040000  // Directory
	c_ISREG = 0100000 // Regular file
)

func Tar(dir string, w *tar.Writer, filter func(string) bool) error {
	if err := filepath.Walk(dir, func(path string, file os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fpath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if filter != nil && !filter(fpath) {
			return nil
		}

		hdr := &tar.Header{
			Name:    fpath,
			Mode:    int64(file.Mode().Perm()),
			ModTime: file.ModTime(),
		}
		if file.IsDir() {
			hdr.Name += "/"
			hdr.Typeflag = tar.TypeDir
			hdr.Mode |= c_ISDIR
		} else if file.Mode().IsRegular() {
			hdr.Size = file.Size()
			hdr.Typeflag = tar.TypeReg
			hdr.Mode |= c_ISREG
		} else {
			return nil
		}

		if err := w.WriteHeader(hdr); err != nil {
			return fmt.Errorf("archiver: error writing %s: %s", fpath, err)
		}

		if file.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err := io.Copy(w, f); err != nil {
			return fmt.Errorf("archiver: error copying %s: %s", fpath, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func Untar(dir string, r *tar.Reader) error {
	for {
		header, err := r.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		filename := filepath.Join(dir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filename, os.FileMode(header.Mode)); err != nil {
				return err
			}
			if err := os.Chmod(filename, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			// if the files are out of order, the dir might not exist yet
			if err := os.MkdirAll(filepath.Dir(filename), os.FileMode(header.Mode|0111)); err != nil {
				return err
			}
			writer, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			defer writer.Close()
			if _, err := io.Copy(writer, r); err != nil {
				return err
			}
		default:
		}
	}
	return nil
}
