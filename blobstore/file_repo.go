package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
	"github.com/stevvooe/resumable"
	"github.com/stevvooe/resumable/sha512"
)

type FileRepo struct {
	db             *postgres.DB
	backends       map[string]Backend
	defaultBackend Backend
}

func NewFileRepo(db *postgres.DB, backends []Backend, defaultBackend string) *FileRepo {
	r := &FileRepo{
		db:       db,
		backends: make(map[string]Backend, len(backends)),
	}
	for _, b := range backends {
		r.backends[b.Name()] = b
	}
	r.defaultBackend = r.backends[defaultBackend]
	return r
}

func (r *FileRepo) getBackend(name string) (Backend, error) {
	if b, ok := r.backends[name]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("blobstore: unknown backend %q", name)
}

func (r *FileRepo) List(dir string) ([]string, error) {
	rows, err := r.db.Query("SELECT substring(name FROM '^' || $1 || '/[^/]+/?') AS path FROM files WHERE name LIKE $1 || '%' AND deleted_at IS NULL GROUP BY path", strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path *string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		if path != nil {
			paths = append(paths, *path)
		}
	}
	return paths, rows.Err()
}

// Get is like Open, except the FileStream is not populated (useful for HEAD requests)
func (r *FileRepo) Get(name string, body bool) (*File, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	var info FileInfo
	var backendName string
	var externalID *string
	if err := tx.QueryRow(
		"SELECT file_id, file_oid, external_id, backend, name, type, size, encode(sha512, 'hex'), updated_at FROM files WHERE name = $1 AND deleted_at IS NULL",
		name,
	).Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Name, &info.Type, &info.Size, &info.ETag, &info.ModTime); err != nil {
		if err == pgx.ErrNoRows {
			err = ErrNotFound
		}
		tx.Rollback()
		return nil, err
	}
	if externalID != nil {
		info.ExternalID = *externalID
	}
	if !body {
		tx.Rollback()
		return &File{FileInfo: info, FileStream: fakeSizeSeekerFileStream{info.Size}}, nil
	}

	backend, err := r.getBackend(backendName)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	stream, err := backend.Open(tx, info, true)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &File{FileInfo: info, FileStream: stream}, nil
}

func (r *FileRepo) Copy(to, from string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	info := FileInfo{
		Name: from,
	}
	var backendName string
	var externalID *string
	var sha512, sha512State []byte
	if err := tx.QueryRow(
		"SELECT file_id, file_oid, external_id, backend, type, size, sha512, sha512_state, updated_at FROM files WHERE name = $1 AND deleted_at IS NULL", from,
	).Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Type, &info.Size, &sha512, &sha512State, &info.ModTime); err != nil {
		if err == pgx.ErrNoRows {
			return ErrNotFound
		}
		tx.Rollback()
		return err
	}
	if externalID != nil {
		info.ExternalID = *externalID
	}
	backend, err := r.getBackend(backendName)
	if err != nil {
		tx.Rollback()
		return err
	}

	toInfo := FileInfo{
		Name: to,
		Type: info.Type,
	}
	if err := tx.QueryRow(
		"INSERT INTO files (backend, name, type, size, sha512, sha512_state) VALUES ($1, $2, $3, $4, $5, $6) RETURNING file_id",
		backendName, to, info.Type, info.Size, sha512, sha512State,
	).Scan(&toInfo.ID); err != nil {
		tx.Rollback()
		return err
	}

	if err := backend.Copy(tx, toInfo, info); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *FileRepo) Put(name string, data io.Reader, offset int64, typ string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	info := FileInfo{
		Name: name,
		Type: typ,
	}
	h := sha512.New().(resumable.Hash)
	backend := r.defaultBackend

create:
	err = tx.QueryRow("INSERT INTO files (name, backend, type) VALUES ($1, $2, $3) RETURNING file_id", name, backend.Name(), typ).Scan(&info.ID)
	if postgres.IsUniquenessError(err, "") {
		tx.Rollback()
		tx, err = r.db.Begin()
		if err != nil {
			return err
		}

		if offset > 0 {
			var backendName string
			var sha512State []byte
			var externalID *string
			// file exists, get details
			if err := tx.QueryRow(
				"SELECT file_id, file_oid, external_id, backend, size, sha512_state FROM files WHERE name = $1 AND deleted_at IS NULL", name,
			).Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Size, &sha512State); err != nil {
				tx.Rollback()
				return err
			}
			if externalID != nil {
				info.ExternalID = *externalID
			}
			backend, err = r.getBackend(backendName)
			if err != nil {
				tx.Rollback()
				return err
			}
			if offset != info.Size {
				tx.Rollback()
				// TODO: pass error via HTTP response
				return fmt.Errorf("blobstore: offset (%d) does not match blob size (%d), unable to append", offset, info.Size)
			}
			if len(sha512State) > 0 {
				err = h.Restore(sha512State)
			}
			if (len(sha512State) == 0 || err != nil || h.Len() != info.Size) && info.Size > 0 {
				// hash state is not resumable, read current data into hash
				f, err := backend.Open(tx, info, false)
				if err != nil {
					tx.Rollback()
					return err
				}
				h.Reset()
				if _, err := io.Copy(h, io.LimitReader(f, info.Size)); err != nil {
					f.Close()
					tx.Rollback()
					return err
				}
				f.Close()
			}
		} else {
			// file exists, not appending, overwrite by deleting
			err = tx.Exec("UPDATE files SET deleted_at = now() WHERE name = $1 AND deleted_at IS NULL", name)
			if err != nil {
				tx.Rollback()
				return err
			}
			goto create
		}
	} else if err != nil {
		tx.Rollback()
		return err
	}

	sr := newSizeReader(data)
	sr.size = info.Size
	if err := backend.Put(tx, info, io.TeeReader(sr, h), offset > 0); err != nil {
		tx.Rollback()
		return err
	}

	sha512State, _ := h.State()
	if err := tx.Exec(
		"UPDATE files SET size = $2, sha512 = $3, sha512_state = $4, updated_at = now() WHERE file_id = $1",
		info.ID, sr.Size(), h.Sum(nil), sha512State,
	); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *FileRepo) Delete(name string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	// use a regular expression so that either a file with the name is
	// deleted, or any file prefixed with "{name}/" is deleted (so in other
	// words, mimic either deleting a file or recursively deleting a
	// directory)
	rows, err := tx.Query(
		"UPDATE files SET deleted_at = now() WHERE name ~ ('^' || $1 || '(/.*)?$') AND deleted_at IS NULL RETURNING file_id, external_id, backend, name", name,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	backendFiles := make(map[string][]FileInfo)
	for rows.Next() {
		var info FileInfo
		var backendName string
		var externalID *string
		if err := rows.Scan(&info.ID, &externalID, &backendName, &info.Name); err != nil {
			tx.Rollback()
			return err
		}
		if externalID != nil {
			info.ExternalID = *externalID
		}
		backendFiles[backendName] = append(backendFiles[backendName], info)
	}
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	var errors []error
	for b, files := range backendFiles {
		backend, err := r.getBackend(b)
		if err != nil {
			errors = append(errors, err)
		}
		for _, f := range files {
			if err := backend.Delete(f); err != nil {
				errors = append(errors, err)
			}
		}
	}
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

type sizeReader struct {
	size int64
	r    io.Reader
}

func (sr *sizeReader) Read(p []byte) (int, error) {
	n, err := sr.r.Read(p)
	sr.size = sr.size + int64(n)
	return n, err
}

func (sr *sizeReader) Size() int64 {
	return sr.size
}

func newSizeReader(r io.Reader) *sizeReader {
	return &sizeReader{r: r}
}

// HACK: work around ServeContent length detection, remove when fixed
type fakeSizeSeekerFileStream struct {
	size int64
}

func (fakeSizeSeekerFileStream) Close() error             { return nil }
func (fakeSizeSeekerFileStream) Read([]byte) (int, error) { return 0, io.EOF }
func (f fakeSizeSeekerFileStream) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == os.SEEK_END {
		return f.size, nil
	}
	return 0, nil
}
