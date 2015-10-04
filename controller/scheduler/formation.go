package main

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/controller/utils"
)

type Formations map[utils.FormationKey]*Formation

func (fs Formations) Get(appID, releaseID string) *Formation {
	return fs[utils.FormationKey{AppID: appID, ReleaseID: releaseID}]
}

func (fs Formations) Add(f *Formation) *Formation {
	if existing, ok := fs[f.key()]; ok {
		return existing
	}
	fs[f.key()] = f
	return f
}

type Processes map[string]int

func (p Processes) Equals(other Processes) bool {
	for typ, count := range p {
		if other[typ] != count {
			return false
		}
	}
	for typ, count := range other {
		if p[typ] != count {
			return false
		}
	}
	return true
}

func (p Processes) IsEmpty() bool {
	for _, count := range p {
		if count != 0 {
			return false
		}
	}
	return true
}

type Formation struct {
	*ct.ExpandedFormation
}

func NewFormation(ef *ct.ExpandedFormation) *Formation {
	return &Formation{
		ExpandedFormation: ef,
	}
}

func (f *Formation) GetProcesses() Processes {
	return Processes(f.Processes)
}

func (f *Formation) key() utils.FormationKey {
	return utils.FormationKey{AppID: f.App.ID, ReleaseID: f.Release.ID}
}

// Update stores the new processes and returns the diff from the previous
// processes.
func (f *Formation) Update(procs Processes) Processes {
	diff := make(map[string]int)
	for typ, requested := range procs {
		if typ == "" {
			continue
		}
		current := f.Processes[typ]
		diff[typ] = requested - current
	}

	for typ, current := range f.Processes {
		if typ == "" {
			continue
		}
		if _, ok := procs[typ]; !ok {
			diff[typ] = -current
		}
	}
	f.Processes = procs
	return diff
}
