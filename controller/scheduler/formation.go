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

	// OriginalProcesses are the processes from the controller formation
	// without any changes for omni jobs so we can recalculate omni counts
	// when host counts change
	OriginalProcesses Processes
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
	f.OriginalProcesses = procs
	f.Processes = procs
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
