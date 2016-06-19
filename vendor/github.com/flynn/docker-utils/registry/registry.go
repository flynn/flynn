package registry

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/flynn/docker-utils/version"
)

type Registry struct {
	Version string
	Path    string
	Info    RegistryInfo
}

// copied from docker/registry around 1.6.0
type RegistryInfo struct {
	Version    string `json:"version"`
	Standalone bool   `json:"standalone"`
}

func (r *Registry) Init() error {
	p, err := filepath.Abs(r.Path)
	if err != nil {
		return err
	}
	r.Path = p

	r.Info.Version = version.VERSION
	r.Info.Standalone = true

	if _, err := os.Stat(r.Path); os.IsNotExist(err) {
		err = os.Mkdir(r.Path, 0755)
		if err != nil {
			return err
		}
	}
	if r.Version == "" {
		r.Version = "v1"
	}

	for _, dir := range []string{"repositories/library", "images"} {
		err = os.MkdirAll(filepath.Join(r.Path, r.Version, dir), 0755)
		if err != nil {
			return err
		}
	}

	if _, err = os.Stat(filepath.Join(r.Path, r.Version, "_ping")); os.IsNotExist(err) {
		fh, err := os.Create(filepath.Join(r.Path, r.Version, "_ping"))
		if err != nil {
			return err
		}
		buf, err := json.Marshal(r.Info)
		if err != nil {
			return err
		}
		_, err = fh.Write(buf)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r Registry) EnsureRepoReady(name string) error {
	if err := os.MkdirAll(r.RepositoryPath(name), 0755); err != nil {
		return err
	}
	if strings.Count(name, "/") == 0 {
		if err := os.Symlink(r.RepositoryPath(name), r.RepositoryPath("library/"+name)); err != nil {
			return err
		}
	}
	return nil
}

func (r Registry) CreateAncestry(hashid string) error {
	// the ancestry starts at the given ID and ends at the scratch layer
	hashes := []string{hashid}

	thisHash := hashid
	for {
		// Unmarshal the json for the layer, get the parent
		imageJson, err := ioutil.ReadFile(r.JsonFileName(thisHash))
		if err != nil {
			return err
		}
		imageData := ImageMetadata{}
		if err = json.Unmarshal(imageJson, &imageData); err != nil {
			return err
		}
		if len(imageData.Parent) == 0 {
			break
		}
		hashes = append(hashes, imageData.Parent)
		thisHash = imageData.Parent
	}

	ancestry_fh, err := os.Create(r.AncestryFileName(hashid))
	if err != nil {
		return err
	}
	defer ancestry_fh.Close()
	hashesJson, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	if _, err = ancestry_fh.Write(hashesJson); err != nil {
		return err
	}
	return nil
}

func (r Registry) HasRepository(name string) bool {
	var hasImages, hasTags bool
	if r.Version == "v1" {
		if s, err := os.Stat(r.ImagesFileName(name)); err == nil && s.Mode().IsRegular() {
			hasImages = true
		}
		if s, err := os.Stat(r.TagsFileName(name)); err == nil && s.Mode().IsRegular() {
			hasTags = true
		}
	}
	return hasImages && hasTags
}

func (r Registry) HasImage(hashid string) bool {
	var hasJson, hasLayer bool
	if r.Version == "v1" {
		if s, err := os.Stat(r.JsonFileName(hashid)); err == nil && s.Mode().IsRegular() {
			hasJson = true
		}
		if s, err := os.Stat(r.LayerFileName(hashid)); err == nil && s.Mode().IsRegular() {
			hasLayer = true
		}
	}
	return hasJson && hasLayer
}

func (r Registry) LayerTarsum(hashid string) (string, error) {
	buf, err := ioutil.ReadFile(r.TarsumFileName(hashid))
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func (r Registry) RepositoryPath(name string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "repositories", name)
	}
	return ""
}

func (r Registry) ImagesFileName(name string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "repositories", name, "images")
	}
	return ""
}

func (r Registry) TagsFileName(name string) string {
	if r.Version == "v1" {
		if strings.Count(name, "/") == 0 {
			name = "library/" + name
		}
		return filepath.Join(r.Path, r.Version, "repositories", name, "tags")
	}
	return ""
}

func (r Registry) JsonFileName(hashid string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "images", hashid, "json")
	}
	return ""
}

func (r Registry) LayerFileName(hashid string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "images", hashid, "layer")
	}
	return ""
}

func (r Registry) TarsumFileName(hashid string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "images", hashid, "tarsum")
	}
	return ""
}

func (r Registry) AncestryFileName(hashid string) string {
	if r.Version == "v1" {
		return filepath.Join(r.Path, r.Version, "images", hashid, "ancestry")
	}
	return ""
}

// for the ./images/ file
type ImageMetadata struct {
	Id     string `json:"id"`
	Parent string `json:"parent"`
}

// for the ./repositories file
type Image struct {
	Checksum string `json:"checksum,omitempty"`
	Id       string `json:"id"`
}

// for the ./repositories file
type Tag struct {
	Layer string `json:"layer"`
	Name  string `json:"name"`
}

func TagsMap(tags []Tag) map[string]string {
	output := map[string]string{}
	for i := range tags {
		output[tags[i].Name] = tags[i].Layer
	}
	return output
}
