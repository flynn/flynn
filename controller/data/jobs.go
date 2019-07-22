package data

import (
	"strings"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/cluster"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

/* Job Stuff */
type JobRepo struct {
	db *postgres.DB
}

func NewJobRepo(db *postgres.DB) *JobRepo {
	return &JobRepo{db}
}

func (r *JobRepo) Get(id string) (*ct.Job, error) {
	if !idPattern.MatchString(id) {
		var err error
		id, err = cluster.ExtractUUID(id)
		if err != nil {
			return nil, ErrNotFound
		}
	}
	row := r.db.QueryRow("job_select", id)
	return scanJob(row)
}

func (r *JobRepo) Add(job *ct.Job) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	// TODO: actually validate
	err = tx.QueryRow(
		"job_insert",
		job.ID,
		job.UUID,
		job.HostID,
		job.AppID,
		job.ReleaseID,
		job.Type,
		string(job.State),
		job.Meta,
		job.ExitStatus,
		job.HostError,
		job.RunAt,
		job.Restarts,
		job.Args,
	).Scan(&job.CreatedAt, &job.UpdatedAt)
	if postgres.IsPostgresCode(err, postgres.CheckViolation) {
		tx.Rollback()
		return ct.ValidationError{Field: "state", Message: err.Error()}
	}
	if err != nil {
		tx.Rollback()
		return err
	}

	for i, volID := range job.VolumeIDs {
		if err := tx.Exec("job_volume_insert", job.UUID, volID, i); err != nil {
			tx.Rollback()
			return err
		}
	}

	// create a job event, ignoring possible duplications
	uniqueID := strings.Join([]string{job.UUID, string(job.State)}, "|")
	if err := tx.Exec("event_insert_unique", job.AppID, job.UUID, uniqueID, string(ct.EventTypeJob), job); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func scanJob(s postgres.Scanner) (*ct.Job, error) {
	job := &ct.Job{}
	var state string
	var volumeIDs string
	err := s.Scan(
		&job.ID,
		&job.UUID,
		&job.HostID,
		&job.AppID,
		&job.ReleaseID,
		&job.Type,
		&state,
		&job.Meta,
		&job.ExitStatus,
		&job.HostError,
		&job.RunAt,
		&job.Restarts,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.Args,
		&volumeIDs,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}
	job.State = ct.JobState(state)
	if volumeIDs != "" {
		job.VolumeIDs = split(volumeIDs[1:len(volumeIDs)-1], ",")
	}
	return job, nil
}

func (r *JobRepo) List(appID string) ([]*ct.Job, error) {
	rows, err := r.db.Query("job_list", appID)
	if err != nil {
		return nil, err
	}
	jobs := []*ct.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *JobRepo) ListActive() ([]*ct.Job, error) {
	rows, err := r.db.Query("job_list_active")
	if err != nil {
		return nil, err
	}
	jobs := []*ct.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
