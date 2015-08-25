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

type formationJobs map[utils.FormationKey]map[string][]*Job

func NewFormationJobs(jobs map[string]*Job) formationJobs {
	fj := make(formationJobs)
	for _, job := range jobs {
		fj.AddJob(job)
	}
	return fj
}

func (fc formationJobs) AddJob(j *Job) {
	key := j.Formation.key()
	_, ok := fc[key]
	if !ok {
		fc[key] = make(map[string][]*Job)
	}
	fc[key][j.Type] = append(fc[key][j.Type], j)
}

type pendingJobs map[utils.FormationKey]map[string]int

func CopyPendingJobs(pending pendingJobs) pendingJobs {
	copied := make(pendingJobs)
	for key, form := range pending {
		copied[key] = make(map[string]int)
		for typ, numJobs := range form {
			copied[key][typ] = numJobs
		}
	}
	return copied
}

func MergePendingJobs(pending1, pending2 pendingJobs) pendingJobs {
	copied := CopyPendingJobs(pending1)

	for key, form := range pending2 {
		if _, ok := copied[key]; !ok {
			copied[key] = make(map[string]int)
		}
		for typ, numJobs := range form {
			copied[key][typ] += numJobs
		}
	}
	return copied
}

func NewPendingJobs(jobs map[string]*Job, pending pendingJobs) pendingJobs {
	fjc := CopyPendingJobs(pending)

	for _, job := range jobs {
		fjc.AddJob(job)
	}
	return fjc
}

func (fc pendingJobs) AddJob(j *Job) {
	key := j.Formation.key()
	_, ok := fc[key]
	if !ok {
		fc[key] = make(map[string]int)
	}
	fc[key][j.Type] += 1
}

func (fc pendingJobs) RemoveJob(j *Job) {
	key := j.Formation.key()
	_, ok := fc[key]
	if !ok {
		fc[key] = make(map[string]int)
	}
	fc[key][j.Type] -= 1
}

func (fc pendingJobs) GetProcesses(key utils.FormationKey) map[string]int {
	return fc[key]
}

func (fc pendingJobs) hasPendingJobs(j *Job) bool {
	if j == nil || j.Formation == nil {
		return false
	}
	key := j.Formation.key()
	if _, ok := fc[key]; !ok {
		return false
	}
	return true
}

func (fc pendingJobs) HasStarts(j *Job) bool {
	if fc.hasPendingJobs(j) {
		return fc[j.Formation.key()][j.Type] > 0
	}
	return false
}

func (fc pendingJobs) HasStops(j *Job) bool {
	if fc.hasPendingJobs(j) {
		return fc[j.Formation.key()][j.Type] < 0
	}
	return false
}
