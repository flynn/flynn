package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"

	ct "github.com/flynn/flynn-controller/types"
	"github.com/flynn/go-crypto-ssh"
	"github.com/flynn/go-sql"
)

type KeyRepo struct {
	db *DB
}

func NewKeyRepo(db *DB) *KeyRepo {
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

	return r.db.QueryRow("INSERT INTO keys (key_id, key, comment) VALUES ($1, $2, $3) RETURNING created_at", key.ID, key.Key, key.Comment).Scan(&key.CreatedAt)
}

func fingerprintKey(key []byte) string {
	digest := md5.Sum(key)
	return hex.EncodeToString(digest[:])
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
	return r.db.Exec("UPDATE keys SET deleted_at = now() WHERE key_id = $1", id)
}

func (r *KeyRepo) List() (interface{}, error) {
	rows, err := r.db.Query("SELECT key_id, key, comment, created_at FROM keys WHERE deleted_at IS NULL ORDER BY created_at DESC")
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
