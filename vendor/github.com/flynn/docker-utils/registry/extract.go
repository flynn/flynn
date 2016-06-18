package registry

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/vbatts/docker-utils/sum"
)

/*
From a tar input, push it to the registry.Registry r
*/
func ExtractTar(r *Registry, in io.Reader) error {
	return extractTar(r, in, true, true)
}

func ExtractTarWithoutTarsums(r *Registry, in io.Reader, compress bool) error {
	return extractTar(r, in, false, compress)
}

func extractTar(r *Registry, in io.Reader, tarsums, compress bool) error {
	t := tar.NewReader(in)

	for {
		hdr, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		basename := filepath.Base(hdr.Name)
		hashid := filepath.Dir(hdr.Name)
		// The json file comes first
		if basename == "json" {
			if r != nil && r.HasImage(hashid) {
				continue
			}
			err = os.MkdirAll(filepath.Dir(r.JsonFileName(hashid)), 0755)
			if err != nil {
				return err
			}
			json_fh, err := os.Create(r.JsonFileName(hashid))
			if err != nil {
				return err
			}
			if _, err = io.Copy(json_fh, t); err != nil {
				return err
			}
			if err = json_fh.Close(); err != nil {
				return err
			}
		} else if basename == "layer.tar" {
			if r != nil && r.HasImage(hashid) {
				continue
			}
			err = os.MkdirAll(filepath.Dir(r.JsonFileName(hashid)), 0755)
			if err != nil {
				return err
			}
			layer_fh, err := os.Create(r.LayerFileName(hashid))
			if err != nil {
				return err
			}
			if !tarsums {
				if compress {
					layer_gz, err := gzip.NewWriterLevel(layer_fh, gzip.BestCompression)
					if err != nil {
						return err
					}
					if _, err = io.Copy(layer_gz, t); err != nil {
						return err
					}
					if err = layer_gz.Close(); err != nil {
						return err
					}
				} else {
					if _, err = io.Copy(layer_fh, t); err != nil {
						return err
					}
				}
				fmt.Printf("Extracted Layer: %s\n", hashid)
				continue
			}
			json_fh, err := os.Open(r.JsonFileName(hashid))
			if err != nil {
				return err
			}
			str, err := sum.SumTarLayer(t, json_fh, layer_fh)
			if err != nil {
				return err
			}
			if err = layer_fh.Close(); err != nil {
				return err
			}
			if err = json_fh.Close(); err != nil {
				return err
			}

			tarsum_fh, err := os.Create(r.TarsumFileName(hashid))
			if err != nil {
				return err
			}
			if _, err = tarsum_fh.WriteString(str); err != nil {
				return err
			}
			if err = tarsum_fh.Close(); err != nil {
				return err
			}
			fmt.Printf("Extracted Layer: %s [%s]\n", hashid, str)
		} else if basename == "repositories" {
			repoMap := map[string]map[string]string{}
			repositoriesJson, err := ioutil.ReadAll(t)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(repositoriesJson, &repoMap); err != nil {
				return err
			}

			for repo, set := range repoMap {
				fmt.Println(repo)
				var (
					images_fh *os.File
					tags_fh   *os.File
					images    = []Image{}
					tags      = []Tag{}
				)
				err = r.EnsureRepoReady(repo)
				if err != nil {
					return err
				}
				if r.HasRepository(repo) {
					images_fh, err = os.Open(r.ImagesFileName(repo))
					if err != nil {
						return err
					}
					imagesJson, err := ioutil.ReadAll(images_fh)
					if err != nil {
						return err
					}
					images_fh.Seek(0, 0) // this will be added to, so the result will always be longer
					if err = json.Unmarshal(imagesJson, &images); err != nil {
						return err
					}

					tags_fh, err = os.Open(r.TagsFileName(repo))
					if err != nil {
						return err
					}
					tagsJson, err := ioutil.ReadAll(tags_fh)
					if err != nil {
						return err
					}
					tags_fh.Seek(0, 0) // this will be added to, so the result will always be longer
					if err = json.Unmarshal(tagsJson, &tags); err != nil {
						return err
					}
				} else {
					images_fh, err = os.Create(r.ImagesFileName(repo))
					if err != nil {
						return err
					}
					tags_fh, err = os.Create(r.TagsFileName(repo))
					if err != nil {
						return err
					}
				}
				for tag, hashid := range set {
					fmt.Printf("  %s :: %s\n", tag, hashid)

					// visting existing tags for this repo. update and merge
					tagExisted := false
					for _, e_tag := range tags {
						if e_tag.Name == tag {
							e_tag.Layer = hashid
							tagExisted = true
						}
					}
					if !tagExisted {
						tags = append(tags, Tag{Name: tag, Layer: hashid})
					}

					imageExisted := false

					var checksum string
					if tarsums {
						checksum, err = r.LayerTarsum(hashid)
						if err != nil {
							return err
						}
					}
					for _, e_image := range images {
						if e_image.Id == hashid {
							imageExisted = true
						}
					}
					if !imageExisted {
						images = append(images, Image{Id: hashid, Checksum: checksum})
					}
				}

				// ensure that each image tagged has an ancestry file
				for _, tag := range tags {
					if _, err = os.Stat(r.AncestryFileName(tag.Layer)); os.IsNotExist(err) {
						r.CreateAncestry(tag.Layer)
					}
				}

				// Write back the new data
				tagsJson, err := json.Marshal(TagsMap(tags))
				if err != nil {
					return err
				}
				imagesJson, err := json.Marshal(images)
				if err != nil {
					return err
				}
				if _, err = tags_fh.Write(tagsJson); err != nil {
					return err
				}
				if tags_fh.Close(); err != nil {
					return err
				}
				if _, err = images_fh.Write(imagesJson); err != nil {
					return err
				}
				if images_fh.Close(); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
