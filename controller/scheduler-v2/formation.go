package main

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
)

type Formations map[utils.FormationKey]*Formation

func (fs Formations) Get(appID, releaseID string) *Formation {
	if form, ok := fs[utils.FormationKey{AppID: appID, ReleaseID: releaseID}]; ok {
		return form
	}
	return nil
}

func (fs Formations) Add(f *Formation) *Formation {
	if existing, ok := fs[f.key()]; ok {
		return existing
	}
	fs[f.key()] = f
	return f
}

type Formation struct {
	*ct.ExpandedFormation
}

func NewFormation(ef *ct.ExpandedFormation) *Formation {
	return &Formation{ef}
}

func (f *Formation) key() utils.FormationKey {
	return utils.FormationKey{f.App.ID, f.Release.ID}
}

// Update stores the new processes and returns the diff from the previous
// processes.
func (f *Formation) Update(procs map[string]int) map[string]int {
	diff := make(map[string]int)
	for typ, requested := range procs {
		current := f.Processes[typ]
		diff[typ] = requested - current
	}

	for typ, current := range f.Processes {
		if _, ok := procs[typ]; !ok {
			diff[typ] = -current
		}
	}
	f.Processes = procs
	return diff
}

type formationCounts map[utils.FormationKey]map[string]int

func NewFormationCounts(fs Formations) formationCounts {
	fc := make(formationCounts)
	for fKey := range fs {
		fc[fKey] = make(map[string]int)
	}
	return fc
}

func (fc formationCounts) AddJob(j *Job) {
	key := j.Formation.key()
	f, ok := fc[key]
	if !ok {
		fc[key] = make(map[string]int)
		f = fc[key]
	}
	f[j.Type]++
}
