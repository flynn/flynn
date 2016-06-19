package main

import (
	"errors"

	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

type ProviderRepo struct {
	db *postgres.DB
}

func NewProviderRepo(db *postgres.DB) *ProviderRepo {
	return &ProviderRepo{db}
}

func (r *ProviderRepo) Add(data interface{}) error {
	p := data.(*ct.Provider)
	if p.Name == "" {
		return errors.New("controller: name must not be blank")
	}
	if p.URL == "" {
		return errors.New("controller: url must not be blank")
	}
	// TODO: validate url
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	err = tx.QueryRow("provider_insert", p.Name, p.URL).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   p.ID,
		ObjectType: ct.EventTypeProvider,
	}, p); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanProvider(s postgres.Scanner) (*ct.Provider, error) {
	p := &ct.Provider{}
	err := s.Scan(&p.ID, &p.Name, &p.URL, &p.CreatedAt, &p.UpdatedAt)
	if err == pgx.ErrNoRows {
		err = ErrNotFound
	}
	return p, err
}

func (r *ProviderRepo) Get(id string) (interface{}, error) {
	var row postgres.Scanner
	if idPattern.MatchString(id) {
		row = r.db.QueryRow("provider_select_by_name_or_id", id, id)
	} else {
		row = r.db.QueryRow("provider_select_by_name", id)
	}
	return scanProvider(row)
}

func (r *ProviderRepo) List() (interface{}, error) {
	rows, err := r.db.Query("provider_list")
	if err != nil {
		return nil, err
	}
	providers := []*ct.Provider{}
	for rows.Next() {
		provider, err := scanProvider(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}
