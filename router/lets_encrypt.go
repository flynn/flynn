package main

import (
	"context"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
	"golang.org/x/crypto/acme/autocert"
)

type letsEncryptCache struct {
	db *postgres.DB
}

// Get returns a certificate data for the specified key.
// If there's no such key, Get returns ErrCacheMiss.
func (l *letsEncryptCache) Get(ctx context.Context, key string) ([]byte, error) {
	var data []byte
	err := l.db.QueryRow("select_lets_encrypt", key).Scan(&data)
	if err != nil {
		if err == pgx.ErrNoRows {
			err = autocert.ErrCacheMiss
		}
		return nil, err
	}
	return data, nil
}

// Put stores the data in the cache under the specified key.
// Underlying implementations may use any data storage format,
// as long as the reverse operation, Get, results in the original data.
func (l *letsEncryptCache) Put(ctx context.Context, key string, data []byte) error {
	return l.db.Exec("insert_lets_encrypt", key, data)
}

// Delete removes a certificate data from the cache under the specified key.
// If there's no such key in the cache, Delete returns nil.
func (l *letsEncryptCache) Delete(ctx context.Context, key string) error {
	return l.db.Exec("delete_lets_encrypt", key)
}
