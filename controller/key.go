package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
)

type KeyRepo struct {
	db *postgres.DB
}

func NewKeyRepo(db *postgres.DB) *KeyRepo {
	return &KeyRepo{db}
}

func (r *KeyRepo) Add(data interface{}) error {
	key := data.(*ct.Key)

	if key.Key == "" {
		return errors.New("controller: key must not be blank")
	}

	pubKey, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(key.Key))
	if err != nil {
		return err
	}

	key.ID = fingerprintKey(pubKey.Marshal())
	key.Key = string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(pubKey)))
	key.Comment = comment

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	err = tx.QueryRow("INSERT INTO keys (fingerprint, key, comment) VALUES ($1, $2, $3) RETURNING created_at", key.ID, key.Key, key.Comment).Scan(&key.CreatedAt)
	if postgres.IsUniquenessError(err, "") {
		tx.Rollback()
		return nil
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   key.ID,
		ObjectType: ct.EventTypeKey,
	}, key); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func fingerprintKey(key []byte) string {
	digest := md5.Sum(key)
	return hex.EncodeToString(digest[:])
}

func scanKey(s postgres.Scanner) (*ct.Key, error) {
	key := &ct.Key{}
	err := s.Scan(&key.ID, &key.Key, &key.Comment, &key.CreatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	return key, err
}

func (r *KeyRepo) getKey(id string) (*ct.Key, error) {
	row := r.db.QueryRow("SELECT fingerprint, key, comment, created_at FROM keys WHERE fingerprint = $1 AND deleted_at IS NULL", id)
	return scanKey(row)
}

func (r *KeyRepo) Get(id string) (interface{}, error) {
	return r.getKey(id)
}

func (r *KeyRepo) Remove(id string) error {
	key, err := r.getKey(id)
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE keys SET deleted_at = now() WHERE fingerprint = $1 AND deleted_at IS NULL", key.ID); err != nil {
		tx.Rollback()
		return err
	}
	if err := createEvent(tx.Exec, &ct.Event{
		ObjectID:   key.ID,
		ObjectType: ct.EventTypeKeyDeletion,
	}, key); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *KeyRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT fingerprint, key, comment, created_at FROM keys WHERE deleted_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	keys := []*ct.Key{}
	for rows.Next() {
		key, err := scanKey(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}
