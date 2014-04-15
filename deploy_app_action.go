package bootstrap

import (
	"bytes"
	"log"
	"text/template"

	ct "github.com/flynn/flynn-controller/types"
)

type DeployAppAction struct {
	ID string `json:"id"`

	*ct.ExpandedFormation
	App       *ct.App        `json:"app"`
	Resources []*ct.Provider `json:"resources"`
}

func init() {
	Register("deploy-app", &DeployAppAction{})
}

func interpolate(s *State, arg string) string {
	t, err := template.New("arg").Parse(arg)
	if err != nil {
		log.Printf("Ignoring error parsing %q as template: %s", arg, err)
		return arg
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, s); err != nil {
		log.Printf("Ignoring error executing %q as template: %s", arg, err)
		return arg
	}
	return buf.String()
}

func interpolateRelease(s *State, r *ct.Release) {
	for k, v := range r.Env {
		r.Env[k] = interpolate(s, v)
	}
	for _, proc := range r.Processes {
		for k, v := range proc.Env {
			proc.Env[k] = interpolate(s, v)
		}
		for i, v := range proc.Cmd {
			proc.Cmd[i] = interpolate(s, v)
		}
	}
}

func (a *DeployAppAction) Run(s *State) error {
	as := &AppState{
		ExpandedFormation: &ct.ExpandedFormation{},
		Resources:         make([]*ct.Resource, 0, len(a.Resources)),
	}
	s.StepData[a.ID] = as

	client, err := s.ControllerClient()
	if err != nil {
		return err
	}

	if err := client.CreateApp(a.App); err != nil {
		return err
	}
	as.App = a.App

	if a.Release.Env == nil {
		a.Release.Env = make(map[string]string)
	}
	interpolateRelease(s, a.Release)
	for _, p := range a.Resources {
		if provider, ok := s.Providers[p.Name]; ok {
			p = provider
		} else {
			if err := client.CreateProvider(p); err != nil {
				return err
			}
			s.Providers[p.Name] = p
		}

		res, err := client.ProvisionResource(&ct.ResourceReq{ProviderID: p.ID})
		if err != nil {
			return err
		}
		as.Resources = append(as.Resources, res)

		for k, v := range res.Env {
			a.Release.Env[k] = v
		}
	}

	if err := client.CreateArtifact(a.Artifact); err != nil {
		return err
	}
	as.Artifact = a.Artifact

	a.Release.ArtifactID = a.Artifact.ID
	if err := client.CreateRelease(a.Release); err != nil {
		return err
	}
	as.Release = a.Release

	formation := &ct.Formation{
		AppID:     a.App.ID,
		ReleaseID: a.Release.ID,
		Processes: a.Processes,
	}
	if err := client.PutFormation(formation); err != nil {
		return err
	}
	as.Formation = formation

	if err := client.SetAppRelease(a.App.ID, a.Release.ID); err != nil {
		return err
	}

	return nil
}

func (a *DeployAppAction) Cleanup(s *State) error {
	// TODO
	return nil
}
