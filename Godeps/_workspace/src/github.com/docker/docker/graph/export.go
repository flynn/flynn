package graph

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path"

	log "github.com/flynn/flynn/Godeps/_workspace/src/github.com/Sirupsen/logrus"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/engine"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/archive"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/docker/pkg/parsers"
)

// CmdImageExport exports all images with the given tag. All versions
// containing the same tag are exported. The resulting output is an
// uncompressed tar ball.
// name is the set of tags to export.
// out is the writer where the images are written to.
func (s *TagStore) CmdImageExport(job *engine.Job) engine.Status {
	if len(job.Args) < 1 {
		return job.Errorf("Usage: %s IMAGE [IMAGE...]\n", job.Name)
	}
	// get image json
	tempdir, err := ioutil.TempDir("", "docker-export-")
	if err != nil {
		return job.Error(err)
	}
	defer os.RemoveAll(tempdir)

	rootRepoMap := map[string]Repository{}
	addKey := func(name string, tag string, id string) {
		log.Debugf("add key [%s:%s]", name, tag)
		if repo, ok := rootRepoMap[name]; !ok {
			rootRepoMap[name] = Repository{tag: id}
		} else {
			repo[tag] = id
		}
	}
	for _, name := range job.Args {
		log.Debugf("Serializing %s", name)
		rootRepo := s.Repositories[name]
		if rootRepo != nil {
			// this is a base repo name, like 'busybox'
			for tag, id := range rootRepo {
				addKey(name, tag, id)
				if err := s.exportImage(job.Eng, id, tempdir); err != nil {
					return job.Error(err)
				}
			}
		} else {
			img, err := s.LookupImage(name)
			if err != nil {
				return job.Error(err)
			}

			if img != nil {
				// This is a named image like 'busybox:latest'
				repoName, repoTag := parsers.ParseRepositoryTag(name)

				// check this length, because a lookup of a truncated has will not have a tag
				// and will not need to be added to this map
				if len(repoTag) > 0 {
					addKey(repoName, repoTag, img.ID)
				}
				if err := s.exportImage(job.Eng, img.ID, tempdir); err != nil {
					return job.Error(err)
				}

			} else {
				// this must be an ID that didn't get looked up just right?
				if err := s.exportImage(job.Eng, name, tempdir); err != nil {
					return job.Error(err)
				}
			}
		}
		log.Debugf("End Serializing %s", name)
	}
	// write repositories, if there is something to write
	if len(rootRepoMap) > 0 {
		rootRepoJson, _ := json.Marshal(rootRepoMap)
		if err := ioutil.WriteFile(path.Join(tempdir, "repositories"), rootRepoJson, os.FileMode(0644)); err != nil {
			return job.Error(err)
		}
	} else {
		log.Debugf("There were no repositories to write")
	}

	fs, err := archive.Tar(tempdir, archive.Uncompressed)
	if err != nil {
		return job.Error(err)
	}
	defer fs.Close()

	if _, err := io.Copy(job.Stdout, fs); err != nil {
		return job.Error(err)
	}
	log.Debugf("End export job: %s", job.Name)
	return engine.StatusOK
}

// FIXME: this should be a top-level function, not a class method
func (s *TagStore) exportImage(eng *engine.Engine, name, tempdir string) error {
	for n := name; n != ""; {
		// temporary directory
		tmpImageDir := path.Join(tempdir, n)
		if err := os.Mkdir(tmpImageDir, os.FileMode(0755)); err != nil {
			if os.IsExist(err) {
				return nil
			}
			return err
		}

		var version = "1.0"
		var versionBuf = []byte(version)

		if err := ioutil.WriteFile(path.Join(tmpImageDir, "VERSION"), versionBuf, os.FileMode(0644)); err != nil {
			return err
		}

		// serialize json
		json, err := os.Create(path.Join(tmpImageDir, "json"))
		if err != nil {
			return err
		}
		job := eng.Job("image_inspect", n)
		job.SetenvBool("raw", true)
		job.Stdout.Add(json)
		if err := job.Run(); err != nil {
			return err
		}

		// serialize filesystem
		fsTar, err := os.Create(path.Join(tmpImageDir, "layer.tar"))
		if err != nil {
			return err
		}
		job = eng.Job("image_tarlayer", n)
		job.Stdout.Add(fsTar)
		if err := job.Run(); err != nil {
			return err
		}

		// find parent
		job = eng.Job("image_get", n)
		info, _ := job.Stdout.AddEnv()
		if err := job.Run(); err != nil {
			return err
		}
		n = info.Get("Parent")
	}
	return nil
}
