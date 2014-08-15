package updater

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
)

type Updater struct {
	*controller.Client
}

func (c *Updater) UpdateApp(name, tag string) error {
	// Get old formation
	release, err := c.GetAppRelease(name)
	if err != nil {
		return err
	}
	formation, err := c.GetFormation(name, release.ID)
	if err != nil {
		return err
	}
	// Create artifact
	artifact := &ct.Artifact{
		Type: "docker",
		URI:  fmt.Sprintf("docker://flynn/%s?tag=%s", name, tag),
	}
	if err := c.CreateArtifact(artifact); err != nil {
		return err
	}
	// Create new release
	release = &ct.Release{ArtifactID: artifact.ID}
	if err := c.CreateRelease(release); err != nil {
		return err
	}
	// Create new formation
	formation.ReleaseID = release.ID
	if err := c.PutFormation(formation); err != nil {
		return err
	}
	// Set app release
	if err := c.SetAppRelease(name, release.ID); err != nil {
		return err
	}

	log.Printf("Updated %s.", name)
	return nil
}

func (c *Updater) Update() error {
	response, err := http.Get("https://gist.githubusercontent.com/archSeer/874a948c41b03307d239/raw/22155c281d2f7325f821b9a179a60efe74e6efda/updates.json")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var components map[string]string
	if err := json.Unmarshal(contents, &components); err != nil {
		return err
	}
	log.Printf(string(contents))
	for name, image := range components {
		log.Printf("%s %s", name, image)
		// if err := c.UpdateApp(name, image); err != nil {
		//  return err
		// }
	}
	return nil
}
