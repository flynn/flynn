package main

import (
	"crypto/sha512"
	"encoding/hex"
	"io"
	"os"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/jackc/pgx"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/status"
)

const UniqueViolation = "23505"

func NewPostgresFilesystem(db *sql.DB) (Filesystem, error) {
	m := postgres.NewMigrations()
	m.Add(1,
		`CREATE TABLE files (
	file_id oid PRIMARY KEY DEFAULT lo_create(0),
	name text UNIQUE NOT NULL,
	size bigint,
	type text,
	digest text,
	created_at timestamp with time zone NOT NULL DEFAULT current_timestamp
);`,
		`CREATE FUNCTION delete_file() RETURNS TRIGGER AS $$
    BEGIN
        PERFORM lo_unlink(OLD.file_id);
        RETURN NULL;
    END;
$$ LANGUAGE plpgsql;`,
		`CREATE TRIGGER delete_file
    AFTER DELETE ON files
    FOR EACH ROW EXECUTE PROCEDURE delete_file();`,
	)
	// TODO(jpg) reuse pkg/postgres connection when converted
	connConf := pgx.ConnConfig{
		Host:     os.Getenv("PGHOST"),
		User:     os.Getenv("PGUSER"),
		Password: os.Getenv("PGPASSWORD"),
		Database: os.Getenv("PGDATABASE"),
	}
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: connConf,
	})
	if err != nil {
		return nil, err
	}
	return &PostgresFilesystem{db: pgxpool}, m.Migrate(db)
}

type PostgresFilesystem struct {
	db *pgx.ConnPool
}

func (p *PostgresFilesystem) Status() status.Status {
	if _, err := p.db.Exec("SELECT 1"); err != nil {
		return status.Unhealthy
	}
	return status.Healthy
}

func (p *PostgresFilesystem) Put(name string, r io.Reader, typ string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}

	var id pgx.Oid
create:
	err = tx.QueryRow("INSERT INTO files (name, type) VALUES ($1, $2) RETURNING file_id", name, typ).Scan(&id)
	if e, ok := err.(pgx.PgError); ok && e.Code == UniqueViolation {
		tx.Rollback()
		tx, err = p.db.Begin()
		if err != nil {
			return err
		}

		// file exists, delete it first
		_, err = tx.Exec("DELETE FROM files WHERE name = $1", name)
		if err != nil {
			tx.Rollback()
			return err
		}
		goto create
	}
	if err != nil {
		tx.Rollback()
		return err
	}

	lo, err := tx.LargeObjects()
	if err != nil {
		tx.Rollback()
		return err
	}
	obj, err := lo.Open(id, pgx.LargeObjectModeWrite)
	if err != nil {
		tx.Rollback()
		return err
	}

	h := sha512.New()
	size, err := io.Copy(obj, io.TeeReader(r, h))
	if err != nil {
		tx.Rollback()
		return err
	}

	digest := hex.EncodeToString(h.Sum(nil))
	_, err = tx.Exec("UPDATE files SET size = $2, digest = $3 WHERE file_id = $1", id, size, digest)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (p *PostgresFilesystem) Delete(name string) error {
	_, err := p.db.Exec("DELETE FROM files WHERE name = $1", name)
	return err
}

func (p *PostgresFilesystem) Open(name string) (File, error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, err
	}

	var f pgFile
	err = tx.QueryRow("SELECT file_id, size, type, digest, created_at FROM files WHERE name = $1",
		name).Scan(&f.id, &f.size, &f.typ, &f.etag, &f.mtime)
	if err != nil {
		tx.Rollback()
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		return nil, err
	}

	lo, err := tx.LargeObjects()
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	f.LargeObject, err = lo.Open(f.id, pgx.LargeObjectModeRead)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	f.tx = tx

	return &f, nil
}

type pgFile struct {
	*pgx.LargeObject
	id    pgx.Oid
	size  int64
	typ   string
	etag  string
	mtime time.Time

	tx *pgx.Tx
}

func (f *pgFile) Size() int64        { return f.size }
func (f *pgFile) ModTime() time.Time { return f.mtime }
func (f *pgFile) Type() string       { return f.typ }
func (f *pgFile) ETag() string       { return f.etag }
func (f *pgFile) Close() error       { return f.tx.Rollback() }
