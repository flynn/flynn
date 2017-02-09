package store

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	ct "github.com/flynn/flynn/controller/types"
)

// LocalStore stores layers in a local directory
type LocalStore struct {
	root     string
	layerDir string
	tmpDir   string
}

func NewLocalStore(root string) (Store, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	layerDir := filepath.Join(root, "layers")
	tmpDir := filepath.Join(root, "tmp")

	for _, dir := range []string{layerDir, tmpDir} {
		if err := os.Mkdir(dir, 0755); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}

	return &LocalStore{
		root:     root,
		layerDir: layerDir,
		tmpDir:   tmpDir,
	}, nil
}

func (l *LocalStore) GetLayer(id string) (*ct.ImageLayer, bool) {
	f, err := os.Open(l.JSONPath(id))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var layer ct.ImageLayer
	return &layer, json.NewDecoder(f).Decode(&layer) == nil
}

func (l *LocalStore) PutLayer(id string, data io.Reader, meta map[string]string) (*ct.ImageLayer, error) {
	layerTmp, err := ioutil.TempFile(l.tmpDir, "layer")
	if err != nil {
		return nil, err
	}
	defer layerTmp.Close()

	h := sha512.New512_256()
	length, err := io.Copy(layerTmp, io.TeeReader(data, h))
	if err != nil {
		os.Remove(layerTmp.Name())
		return nil, err
	}

	layerPath := l.LayerPath(id)
	if err := os.Rename(layerTmp.Name(), layerPath); err != nil {
		os.Remove(layerTmp.Name())
		return nil, err
	}
	if err := os.Chmod(layerPath, 0644); err != nil {
		os.Remove(layerPath)
		return nil, err
	}

	jsonTmp, err := ioutil.TempFile(l.tmpDir, "layer-json")
	if err != nil {
		os.Remove(layerPath)
		return nil, err
	}
	defer jsonTmp.Close()

	layer := &ct.ImageLayer{
		ID:     id,
		Type:   ct.ImageLayerTypeSquashfs,
		Length: length,
		Hashes: map[string]string{
			"sha512_256": hex.EncodeToString(h.Sum(nil)),
		},
		Meta: meta,
	}

	if err := json.NewEncoder(jsonTmp).Encode(layer); err != nil {
		os.Remove(layerPath)
		os.Remove(jsonTmp.Name())
		return nil, err
	}

	if err := os.Rename(jsonTmp.Name(), l.JSONPath(id)); err != nil {
		os.Remove(layerPath)
		os.Remove(jsonTmp.Name())
		return nil, err
	}

	return layer, nil
}

func (l *LocalStore) LayerURLTemplate() string {
	return fmt.Sprintf("file://%s/{id}.squashfs", l.layerDir)
}

func (l *LocalStore) LayerPath(id string) string {
	return filepath.Join(l.layerDir, id+".squashfs")
}

func (l *LocalStore) JSONPath(id string) string {
	return filepath.Join(l.layerDir, id+".json")
}
