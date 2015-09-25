package main

import (
	"reflect"

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

func (fs Formations) TriggerRectify(key utils.FormationKey) {
	if f, ok := fs[key]; ok {
		select {
		case f.ch <- key:
		default:
		}
	}
}

func (fs Formations) CaseHandlers() CaseHandlers {
	cases := make(CaseHandlers, 0, len(fs))
	for _, f := range fs {
		cases = append(cases, f.CaseHandler)
	}
	return cases
}

func (fs Formations) Remove(key utils.FormationKey) {
	delete(fs, key)
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

type Formation struct {
	*ct.ExpandedFormation
	ch          chan utils.FormationKey
	CaseHandler CaseHandler
}

func NewFormation(ef *ct.ExpandedFormation, handler func(interface{}) error) *Formation {
	ch := make(chan utils.FormationKey, 1)
	return &Formation{
		ExpandedFormation: ef,
		ch:                ch,
		CaseHandler: CaseHandler{
			sc: reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(ch),
			},
			handler: handler,
		},
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

func (f *Formation) IsEmpty() bool {
	for _, count := range f.Processes {
		if count > 0 {
			return false
		}
	}
	return true
}
