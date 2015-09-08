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
	return utils.FormationKey{AppID: f.App.ID, ReleaseID: f.Release.ID}
}

// Update stores the new processes and returns the diff from the previous
// processes.
func (f *Formation) Update(procs map[string]int) map[string]int {
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

type typePendingJobs map[string]int
type formPendingJobs map[string]typePendingJobs
type pendingJobs map[utils.FormationKey]formPendingJobs

func (pj pendingJobs) Clone() pendingJobs {
	copied := make(pendingJobs, len(pj))
	for key, form := range pj {
		copied[key] = make(formPendingJobs, len(form))
		for typ, hosts := range form {
			copied[key][typ] = make(typePendingJobs, len(hosts))
			for hostID, numJobs := range hosts {
				copied[key][typ][hostID] = numJobs
			}
		}
	}
	return copied
}

func (pj pendingJobs) Update(other pendingJobs) {
	for key, form := range other {
		if _, ok := pj[key]; !ok {
			pj[key] = make(formPendingJobs, len(form))
		}
		for typ, hosts := range form {
			if _, ok := pj[key][typ]; !ok {
				pj[key][typ] = make(typePendingJobs, len(hosts))
			}
			for hostID, numJobs := range hosts {
				pj[key][typ][hostID] += numJobs
			}
		}
	}
}

func NewPendingJobs(jobs map[string]*Job) pendingJobs {
	fjc := make(pendingJobs)

	for _, job := range jobs {
		fjc.AddJob(job)
	}
	return fjc
}

func (fc pendingJobs) AddJob(j *Job) {
	key := j.Formation.key()
	if _, ok := fc[key]; !ok {
		fc[key] = make(formPendingJobs)
	}
	if _, ok := fc[key][j.Type]; !ok {
		fc[key][j.Type] = make(typePendingJobs)
	}
	fc[key][j.Type][j.HostID] += 1
}

func (fc pendingJobs) RemoveJob(j *Job) {
	key := j.Formation.key()
	if _, ok := fc[key]; !ok {
		fc[key] = make(formPendingJobs)
	}
	if _, ok := fc[key][j.Type]; !ok {
		fc[key][j.Type] = make(typePendingJobs)
	}
	fc[key][j.Type][j.HostID] -= 1
}

func (fc pendingJobs) GetProcesses(key utils.FormationKey) map[string]int {
	procs := make(map[string]int)
	for typ, hosts := range fc[key] {
		for _, numJobs := range hosts {
			procs[typ] += numJobs
			if procs[typ] == 0 {
				delete(procs, typ)
			}
		}
	}
	return procs
}

func (fc pendingJobs) GetHostJobCounts(key utils.FormationKey, typ string) map[string]int {
	counts := make(map[string]int)
	hosts, ok := fc[key][typ]
	if !ok {
		return counts
	}
	for h, count := range hosts {
		counts[h] += count
	}
	return counts
}

func (fc pendingJobs) HasStarts(j *Job) bool {
	if j == nil || j.Formation == nil {
		return false
	}
	key := j.Formation.key()
	if _, ok := fc[key]; !ok {
		return false
	}
	if _, ok := fc[key][j.Type]; !ok {
		return false
	}
	return fc[j.Formation.key()][j.Type][j.HostID] > 0
}
