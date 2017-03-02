package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cluster"
)

// TODO: prune old jobs?

// ErrJobExists is returned when attempting to add a job to the state with an
// ID which already exists
var ErrJobExists = errors.New("job already exists")

type ErrVolumesInUse struct {
	volIDs []string
}

func (e ErrVolumesInUse) Error() string {
	return fmt.Sprintf("volumes in use: %s", strings.Join(e.volIDs, ", "))
}

type State struct {
	id string

	jobs map[string]*host.ActiveJob
	mtx  sync.RWMutex

	listeners map[string]map[chan host.Event]struct{} // job id -> listener list (ID "all" gets all events)
	listenMtx sync.RWMutex
	attachers map[string]map[chan struct{}]struct{}

	stateFilePath string
	stateDB       *bolt.DB
	dbUsers       int
	dbCond        *sync.Cond

	backend Backend
}

func NewState(id string, stateFilePath string) *State {
	return &State{
		id:            id,
		stateFilePath: stateFilePath,
		jobs:          make(map[string]*host.ActiveJob),
		listeners:     make(map[string]map[chan host.Event]struct{}),
		attachers:     make(map[string]map[chan struct{}]struct{}),
		dbCond:        sync.NewCond(&sync.Mutex{}),
	}
}

/*
	Restore prior state from the save location defined at construction time.
	If the state save file is empty, nothing is loaded, and no error is returned.
*/
func (s *State) Restore(backend Backend, buffers host.LogBuffers) (func(), error) {
	if err := s.Acquire(); err != nil {
		return nil, err
	}
	defer s.Release()

	s.backend = backend

	var resurrect []*host.Job
	if err := s.stateDB.View(func(tx *bolt.Tx) error {
		jobsBucket := tx.Bucket([]byte("jobs"))
		backendJobsBucket := tx.Bucket([]byte("backend-jobs"))
		backendGlobalBucket := tx.Bucket([]byte("backend-global"))
		persistentBucket := tx.Bucket([]byte("persistent-jobs"))

		// restore jobs
		if err := jobsBucket.ForEach(func(k, v []byte) error {
			job := &host.ActiveJob{}
			if err := json.Unmarshal(v, job); err != nil {
				return err
			}
			if job.CreatedAt.IsZero() {
				job.CreatedAt = time.Now().UTC()
			}
			s.jobs[string(k)] = job

			return nil
		}); err != nil {
			return err
		}

		// hand opaque blobs back to backend so it can do its restore
		backendJobsBlobs := make(map[string][]byte)
		if err := backendJobsBucket.ForEach(func(k, v []byte) error {
			backendJobsBlobs[string(k)] = v
			return nil
		}); err != nil {
			return err
		}
		backendGlobalBlob := backendGlobalBucket.Get([]byte("backend"))
		if err := backend.UnmarshalState(s.jobs, backendJobsBlobs, backendGlobalBlob, buffers); err != nil {
			return err
		}

		// resurrect any persistent jobs which are not running
		if err := persistentBucket.ForEach(func(k, v []byte) error {
			for _, job := range s.jobs {
				if job.Job.ID == string(v) && !backend.JobExists(job.Job.ID) {
					resurrect = append(resurrect, job.Job)
				}
			}
			return nil
		}); err != nil {
			return err
		}

		return nil
	}); err != nil && err != io.EOF {
		return nil, fmt.Errorf("could not restore from host persistence db: %s", err)
	}

	return func() {
		if len(resurrect) == 0 {
			return
		}
		var wg sync.WaitGroup
		wg.Add(len(resurrect))
		for _, job := range resurrect {
			go func(job *host.Job) {
				// generate a new job id, this is a new job
				newJob := job.Dup()
				newJob.ID = cluster.GenerateJobID(s.id, "")
				if _, ok := newJob.Config.Env["FLYNN_JOB_ID"]; ok {
					newJob.Config.Env["FLYNN_JOB_ID"] = newJob.ID
				}
				log.Printf("resurrecting %s as %s", job.ID, newJob.ID)
				s.AddJob(newJob)
				backend.Run(newJob, nil, nil)
				wg.Done()
			}(job)
		}
		wg.Wait()
	}, nil
}

// OpenDB opens and initialises the persistence DB, if not already open.
func (s *State) OpenDB() error {
	s.dbCond.L.Lock()
	defer s.dbCond.L.Unlock()

	if s.stateDB != nil {
		return nil
	}

	// open/initialize db
	if err := os.MkdirAll(filepath.Dir(s.stateFilePath), 0755); err != nil {
		return fmt.Errorf("could not not mkdir for db: %s", err)
	}
	stateDB, err := bolt.Open(s.stateFilePath, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("could not open db: %s", err)
	}
	s.stateDB = stateDB
	if err := s.stateDB.Update(func(tx *bolt.Tx) error {
		// idempotently create buckets.  (errors ignored because they're all compile-time impossible args checks.)
		tx.CreateBucketIfNotExists([]byte("jobs"))
		tx.CreateBucketIfNotExists([]byte("backend-jobs"))
		tx.CreateBucketIfNotExists([]byte("backend-global"))
		tx.CreateBucketIfNotExists([]byte("persistent-jobs"))
		return nil
	}); err != nil {
		return fmt.Errorf("could not initialize host persistence db: %s", err)
	}
	return nil
}

// CloseDB closes the persistence DB, waiting for the state to be fully
// released first.
func (s *State) CloseDB() error {
	s.dbCond.L.Lock()
	defer s.dbCond.L.Unlock()
	if s.stateDB == nil {
		return nil
	}
	for s.dbUsers > 0 {
		s.dbCond.Wait()
	}
	if err := s.stateDB.Close(); err != nil {
		return err
	}
	s.stateDB = nil
	return nil
}

var ErrDBClosed = errors.New("state DB closed")

// Acquire acquires the state for use by incrementing s.dbUsers, which prevents
// the state DB being closed until the caller has finished performing actions
// which will lead to changes being persisted to the DB.
//
// For example, running a job starts the job and then persists the change of
// state, but if the DB is closed in that time then the state of the running
// job will be lost.
//
// ErrDBClosed is returned if the DB is already closed so API requests will
// fail before any actions are performed.
func (s *State) Acquire() error {
	s.dbCond.L.Lock()
	defer s.dbCond.L.Unlock()
	if s.stateDB == nil {
		return ErrDBClosed
	}
	s.dbUsers++
	return nil
}

// Release releases the state by decrementing s.dbUsers, broadcasting the
// condition variable if no users are left to wake CloseDB.
func (s *State) Release() {
	s.dbCond.L.Lock()
	defer s.dbCond.L.Unlock()
	s.dbUsers--
	if s.dbUsers == 0 {
		s.dbCond.Broadcast()
	}
}

func (s *State) PersistBackendGlobalState(data []byte) error {
	return s.stateDB.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("backend-global")).Put([]byte("backend"), data)
	})
}

func (s *State) persist(jobID string) {
	// s.mtx.RLock() should already be covered by caller

	if err := s.stateDB.Update(func(tx *bolt.Tx) error {
		jobsBucket := tx.Bucket([]byte("jobs"))
		backendJobsBucket := tx.Bucket([]byte("backend-jobs"))
		backendGlobalBucket := tx.Bucket([]byte("backend-global"))

		// serialize the changed job, and push it into jobs bucket
		if _, exists := s.jobs[jobID]; exists {
			b, err := json.Marshal(s.jobs[jobID])
			if err != nil {
				return fmt.Errorf("failed to serialize job state: %s", err)
			}
			err = jobsBucket.Put([]byte(jobID), b)
			if err != nil {
				return fmt.Errorf("could not persist job to boltdb: %s", err)
			}
		} else {
			jobsBucket.Delete([]byte(jobID))
		}

		// save the opaque blob the backend provides regarding this job if it is starting/running
		if backend, ok := s.backend.(JobStateSaver); ok {
			if job, exists := s.jobs[jobID]; exists && (job.Status == host.StatusStarting || job.Status == host.StatusRunning) {
				backendState, err := backend.MarshalJobState(jobID)
				if err != nil {
					return fmt.Errorf("backend failed to serialize job state: %s", err)
				}
				if backendState == nil {
					backendJobsBucket.Delete([]byte(jobID))
				} else {
					err = backendJobsBucket.Put([]byte(jobID), backendState)
					if err != nil {
						return fmt.Errorf("could not persist backend job state to boltdb: %s", err)
					}
				}
			} else {
				backendJobsBucket.Delete([]byte(jobID))
			}
		}

		// (re)save any state the backend provides that isn't tied to specific jobs.
		if backend, ok := s.backend.(StateSaver); ok {
			bytes, err := backend.MarshalGlobalState()
			if err != nil {
				return fmt.Errorf("backend failed to serialize global state: %s", err)
			}
			err = backendGlobalBucket.Put([]byte("backend"), bytes)
			if err != nil {
				return fmt.Errorf("could not persist backend global state to boltdb: %s", err)
			}
		}

		return nil
	}); err != nil {
		panic(fmt.Errorf("could not persist to boltdb: %s", err))
	}
}

func (s *State) AddJob(j *host.Job) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.jobs[j.ID]; ok {
		return ErrJobExists
	}

	// ensure none of the volumes are in use by running jobs
	var volsInUse []string
	for _, vol := range j.Config.Volumes {
		for _, job := range s.jobs {
			if job.Status != host.StatusStarting && job.Status != host.StatusRunning {
				continue
			}
			for _, v := range job.Job.Config.Volumes {
				if v.VolumeID == vol.VolumeID {
					volsInUse = append(volsInUse, vol.VolumeID)
				}
			}
		}
	}
	if len(volsInUse) > 0 {
		return ErrVolumesInUse{volsInUse}
	}

	job := &host.ActiveJob{
		Job:       j,
		HostID:    s.id,
		CreatedAt: time.Now().UTC(),
	}
	s.jobs[j.ID] = job
	s.sendEvent(job, host.JobEventCreate)
	s.persist(j.ID)
	return nil
}

func (s *State) GetJob(id string) *host.ActiveJob {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	job := s.jobs[id]
	if job == nil {
		return nil
	}
	return job.Dup()
}

func (s *State) RemoveJob(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	delete(s.jobs, jobID)
	s.persist(jobID)
}

func (s *State) Get() map[string]*host.ActiveJob {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	res := make(map[string]*host.ActiveJob, len(s.jobs))
	for k, v := range s.jobs {
		res[k] = v.Dup()
	}
	return res
}

func (s *State) GetActive() map[string]*host.ActiveJob {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	res := make(map[string]*host.ActiveJob)
	for id, job := range s.jobs {
		if job.Status == host.StatusStarting || job.Status == host.StatusRunning {
			res[id] = job.Dup()
		}
	}
	return res
}

func (s *State) ClusterJobs() []*host.Job {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	res := make([]*host.Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		res = append(res, j.Job)
	}
	return res
}

func (s *State) SetContainerIP(jobID string, ip net.IP) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.jobs[jobID].InternalIP = ip.String()
	s.persist(jobID)
}

func (s *State) SetContainerPID(jobID string, pid int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.jobs[jobID].PID = &pid
	s.persist(jobID)
}

func (s *State) SetForceStop(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return
	}

	job.ForceStop = true
	s.persist(jobID)
}

func (s *State) SetStatusRunning(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status != host.StatusStarting {
		return
	}

	job.StartedAt = time.Now().UTC()
	job.Status = host.StatusRunning
	s.sendEvent(job, host.JobEventStart)
	if err := s.Acquire(); err == nil {
		s.persist(jobID)
		s.Release()
	}
}

func (s *State) SetStatusDone(jobID string, exitStatus int) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		fmt.Println("SKIP")
		return
	}
	if job.Status == host.StatusDone || job.Status == host.StatusCrashed || job.Status == host.StatusFailed {
		return
	}
	job.EndedAt = time.Now().UTC()
	job.ExitStatus = &exitStatus
	if exitStatus == 0 {
		job.Status = host.StatusDone
	} else {
		job.Status = host.StatusCrashed
	}
	job.PID = nil
	s.sendEvent(job, host.JobEventStop)
	if err := s.Acquire(); err == nil {
		s.persist(job.Job.ID)
		s.Release()
	}
}

func (s *State) SetStatusFailed(jobID string, err error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	job, ok := s.jobs[jobID]
	if !ok || job.Status == host.StatusDone || job.Status == host.StatusCrashed || job.Status == host.StatusFailed {
		return
	}
	job.Status = host.StatusFailed
	job.EndedAt = time.Now().UTC()
	errStr := err.Error()
	job.Error = &errStr
	job.PID = nil
	s.sendEvent(job, host.JobEventError)
	if err := s.Acquire(); err == nil {
		s.persist(jobID)
		s.Release()
	}
	go s.WaitAttach(jobID)
}

func (s *State) AddAttacher(jobID string, ch chan struct{}) *host.ActiveJob {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		jobCopy := *job
		return &jobCopy
	}
	if _, ok := s.attachers[jobID]; !ok {
		s.attachers[jobID] = make(map[chan struct{}]struct{})
	}
	s.attachers[jobID][ch] = struct{}{}
	return nil
}

func (s *State) RemoveAttacher(jobID string, ch chan struct{}) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if a, ok := s.attachers[jobID]; ok {
		delete(a, ch)
		if len(a) == 0 {
			delete(s.attachers, jobID)
		}
	}
}

func (s *State) WaitAttach(jobID string) {
	s.mtx.Lock()
	a := s.attachers[jobID]
	delete(s.attachers, jobID)
	s.mtx.Unlock()
	for ch := range a {
		// signal attach
		ch <- struct{}{}
		// wait for attach
		<-ch
	}
}

func (s *State) AddListener(jobID string) chan host.Event {
	ch := make(chan host.Event)
	s.listenMtx.Lock()
	if _, ok := s.listeners[jobID]; !ok {
		s.listeners[jobID] = make(map[chan host.Event]struct{})
	}
	s.listeners[jobID][ch] = struct{}{}
	s.listenMtx.Unlock()
	return ch
}

func (s *State) RemoveListener(jobID string, ch chan host.Event) {
	go func() {
		// drain to prevent deadlock while removing the listener
		for range ch {
		}
	}()
	s.listenMtx.Lock()
	delete(s.listeners[jobID], ch)
	if len(s.listeners[jobID]) == 0 {
		delete(s.listeners, jobID)
	}
	s.listenMtx.Unlock()
	close(ch)
}

func (s *State) SendCleanupEvent(jobID string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if job, ok := s.jobs[jobID]; ok {
		s.sendEvent(job, host.JobEventCleanup)
	}
}

func (s *State) sendEvent(job *host.ActiveJob, event host.JobEventType) {
	j := job.Dup()
	go func() {
		s.listenMtx.RLock()
		defer s.listenMtx.RUnlock()
		e := host.Event{JobID: job.Job.ID, Job: j, Event: event}
		for ch := range s.listeners["all"] {
			ch <- e
		}
		for ch := range s.listeners[job.Job.ID] {
			ch <- e
		}
	}()
}

func (s *State) SetPersistentSlot(slot string, jobID string) error {
	if err := s.Acquire(); err != nil {
		return err
	}
	defer s.Release()
	return s.stateDB.Update(func(tx *bolt.Tx) error {
		persistentBucket := tx.Bucket([]byte("persistent-jobs"))
		return persistentBucket.Put([]byte(slot), []byte(jobID))
	})
}
