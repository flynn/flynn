package main

import (
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	ct "github.com/flynn/flynn-controller/types"
)

type KeyRepo struct {
	db *DB
}

func NewKeyRepo(db *DB) *KeyRepo {
	return &KeyRepo{db}
}

func (r *KeyRepo) Add(data interface{}) error {
	key := data.(*ct.Key)
	// TODO: validate key type
	if key.Key == "" {
		return errors.New("controller: key must not be blank")
	}

	splitKey := strings.SplitN(key.Key, " ", 3)
	if len(splitKey) < 2 {
		return errors.New("controller: key is missing data")
	}
	fingerprint, err := fingerprintKey(splitKey[1])
	if err != nil {
		return errors.New("controller: error decoding key data")
	}

	key.ID = fingerprint
	key.Key = splitKey[0] + " " + splitKey[1]
	if len(splitKey) > 2 {
		key.Comment = splitKey[2]
	}

	return r.db.QueryRow("INSERT INTO keys (key_id, key, comment) VALUES ($1, $2, $3) RETURNING created_at", key.ID, key.Key, key.Comment).Scan(&key.CreatedAt)
}

func fingerprintKey(key string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}
	digest := md5.Sum(data)
	return hex.EncodeToString(digest[:]), nil
}

func scanKey(s Scanner) (*ct.Key, error) {
	key := &ct.Key{}
	err := s.Scan(&key.ID, &key.Key, &key.Comment, &key.CreatedAt)
	if err == sql.ErrNoRows {
		err = ErrNotFound
	}
	return key, err
}

func (r *KeyRepo) Get(id string) (interface{}, error) {
	row := r.db.QueryRow("SELECT key_id, key, comment, created_at FROM keys WHERE key_id = $1 AND deleted_at IS NULL", id)
	return scanKey(row)
}

func (r *KeyRepo) Remove(id string) error {
	return r.db.Exec("UPDATE keys SET deleted_at = current_timestamp WHERE key_id = $1", id)
}

func (r *KeyRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT key_id, key, comment, created_at FROM keys WHERE deleted_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	var keys []*ct.Key
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
