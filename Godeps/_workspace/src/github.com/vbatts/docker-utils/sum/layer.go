package sum

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"path"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/tarsum"
)

// for existing usage
func SumAllDockerSave(saved io.Reader) (map[string]string, error) {
	return SumAllDockerSaveVersioned(saved, tarsum.Version0)
}

// .. this is an all-in-one. I wish this could be an iterator.
func SumAllDockerSaveVersioned(saved io.Reader, v tarsum.Version) (map[string]string, error) {
	tarRdr := tar.NewReader(saved)
	hashes := map[string]string{}
	jsons := map[string][]byte{}
	for {
		hdr, err := tarRdr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if path.Base(hdr.Name) == "json" {
			id := path.Dir(hdr.Name)
			jsonBuf, err := ioutil.ReadAll(tarRdr)
			if err != nil {
				if err == io.EOF {
					continue
				}
				return nil, err
			}
			jsons[id] = jsonBuf
		}
		if path.Base(hdr.Name) == "layer.tar" {
			id := path.Dir(hdr.Name)
			jsonRdr := bytes.NewReader(jsons[id])
			delete(jsons, id)
			sum, err := SumTarLayerVersioned(tarRdr, jsonRdr, nil, v)
			if err != nil {
				if err == io.EOF {
					continue
				}
				return nil, err
			}
			hashes[id] = sum
		}
	}
	return hashes, nil
}

// for existing usage
func SumTarLayer(tarReader io.Reader, json io.Reader, out io.Writer) (string, error) {
	return SumTarLayerVersioned(tarReader, json, out, tarsum.Version0)
}

// if out is not nil, then the tar input is written there instead
func SumTarLayerVersioned(tarReader io.Reader, json io.Reader, out io.Writer, v tarsum.Version) (string, error) {
	var writer io.Writer = ioutil.Discard
	if out != nil {
		writer = out
	}
	ts, err := tarsum.NewTarSum(tarReader, false, v)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(writer, ts)
	if err != nil {
		return "", err
	}

	var buf []byte
	if json != nil {
		if buf, err = ioutil.ReadAll(json); err != nil {
			return "", err
		}
	}

	return ts.Sum(buf), nil
}
