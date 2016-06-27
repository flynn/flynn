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

func (p Processes) Diff(other Processes) Processes {
	diff := make(map[string]int)
	for typ, count := range p {
		if typ == "" {
			continue
		}
		diff[typ] = count - other[typ]
	}

	for typ, count := range other {
		if typ == "" {
			continue
		}
		if _, ok := p[typ]; !ok {
			diff[typ] = -count
		}
	}
	return diff
}

func (p Processes) Equals(other Processes) bool {
	return p.Diff(other).IsEmpty()
}

func (p Processes) IsEmpty() bool {
	for _, count := range p {
		if count != 0 {
			return false
		}
	}
	return true
}

// IsScaleDownOf returns whether a diff is the complete scale down of any
// process types in the given processes
func (p Processes) IsScaleDownOf(proc Processes) bool {
	for typ, count := range p {
		if count <= -proc[typ] {
			return true
		}
	}
	return false
}

type Formation struct {
	*ct.ExpandedFormation

	// OriginalProcesses are the processes from the controller formation
	// without any changes for omni jobs so we can recalculate omni counts
	// when host counts change
	OriginalProcesses Processes `json:"original_processes"`
}

func NewFormation(ef *ct.ExpandedFormation) *Formation {
	originalProcs := make(Processes, len(ef.Processes))
	for typ, count := range ef.Processes {
		originalProcs[typ] = count
	}
	return &Formation{
		ExpandedFormation: ef,
		OriginalProcesses: originalProcs,
	}
}

// RectifyOmni updates the process counts for omni jobs by multiplying them by
// the host count, returning whether or not any counts have changed
func (f *Formation) RectifyOmni(hostCount int) bool {
	changed := false
	for typ, proc := range f.Release.Processes {
		if proc.Omni && f.Processes != nil && f.Processes[typ] > 0 {
			count := f.OriginalProcesses[typ] * hostCount
			if f.Processes[typ] != count {
				f.Processes[typ] = count
				changed = true
			}
		}
	}
	return changed
}

func (f *Formation) GetProcesses() Processes {
	return Processes(f.Processes)
}

func (f *Formation) SetProcesses(procs Processes) {
	// copy to original processes so they are not modified by RectifyOmni
	f.OriginalProcesses = make(Processes, len(procs))
	for typ, count := range procs {
		f.OriginalProcesses[typ] = count
	}

	f.Processes = procs
}

func (f *Formation) key() utils.FormationKey {
	return utils.FormationKey{AppID: f.App.ID, ReleaseID: f.Release.ID}
}

// Diff returns the diff between the given running processes and what is
// expected to be running for the formation
func (f *Formation) Diff(running Processes) Processes {
	return Processes(f.Processes).Diff(running)
}
