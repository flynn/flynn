package data

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/flynn/flynn/blobstore/backend"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
	"github.com/stevvooe/resumable"
	"github.com/stevvooe/resumable/sha512"
)

var configEnvPattern = regexp.MustCompile(`^BACKEND_([A-Z0-9]+)$`)

func NewFileRepoFromEnv(db *postgres.DB) (*FileRepo, error) {
	backends := []backend.Backend{backend.Postgres}
	environment := os.Environ()
	for _, env := range environment {
		nameInfo := strings.SplitN(env, "=", 2)
		nameMatch := configEnvPattern.FindStringSubmatch(nameInfo[0])
		if len(nameMatch) < 2 {
			continue
		}
		name := strings.ToLower(nameMatch[1])
		info, err := parseBackendInfo(environment, name, nameInfo[1])
		if err != nil {
			return nil, err
		}
		newBackend, ok := backend.Backends[info["backend"]]
		if !ok {
			return nil, fmt.Errorf("blobstore: unknown backend %q for %s", info["backend"], name)
		}
		b, err := newBackend(name, info)
		if err != nil {
			return nil, err
		}
		log.Printf("Configured additional backend: %s (%s)\n", name, info["backend"])
		backends = append(backends, b)
	}

	defaultBackend := "postgres"
	if d := os.Getenv("DEFAULT_BACKEND"); d != "" {
		defaultBackend = d
		var found bool
		for _, b := range backends {
			if b.Name() == d {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("error: unknown default backend %q", d)
		}
	}
	return NewFileRepo(db, backends, defaultBackend), nil
}

func parseBackendInfo(environment []string, name, params string) (map[string]string, error) {
	info := make(map[string]string)
	for _, token := range strings.Split(params, " ") {
		if token == "" {
			continue
		}
		kv := strings.SplitN(token, "=", 2)
		if len(kv) < 2 {
			return nil, fmt.Errorf("blobstore: error parsing backend kv pair %q", token)
		}
		info[kv[0]] = kv[1]
	}
	prefix := strings.ToUpper(fmt.Sprintf("BACKEND_%s_", name))
	for _, env := range environment {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		kv := strings.SplitN(env, "=", 2)
		k := strings.ToLower(strings.TrimPrefix(kv[0], prefix))
		info[k] = kv[1]
	}
	return info, nil
}

type FileRepo struct {
	db             *postgres.DB
	backends       map[string]backend.Backend
	defaultBackend backend.Backend
}

func NewFileRepo(db *postgres.DB, backends []backend.Backend, defaultBackend string) *FileRepo {
	r := &FileRepo{
		db:       db,
		backends: make(map[string]backend.Backend, len(backends)),
	}
	for _, b := range backends {
		r.backends[b.Name()] = b
	}
	r.defaultBackend = r.backends[defaultBackend]
	return r
}

func (r *FileRepo) getBackend(name string) (backend.Backend, error) {
	if b, ok := r.backends[name]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("blobstore: unknown backend %q", name)
}

func (r *FileRepo) DefaultBackend() backend.Backend {
	return r.defaultBackend
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

type BackendFile struct {
	backend.Backend
	backend.FileInfo
}

func (r *FileRepo) ListFilesExcludingDefaultBackend(prefix string) ([]BackendFile, error) {
	rows, err := r.db.Query(
		"SELECT file_id, file_oid, external_id, backend, name, type, size, sha512, updated_at FROM files WHERE backend != $1 AND NAME LIKE $2 || '%' AND deleted_at IS NULL",
		r.defaultBackend.Name(), prefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []BackendFile
	for rows.Next() {
		var info backend.FileInfo
		var backendName string
		var externalID *string
		var sha512 []byte
		if rows.Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Name, &info.Type, &info.Size, &sha512, &info.ModTime); err != nil {
			return nil, err
		}
		if externalID != nil {
			info.ExternalID = *externalID
		}
		info.ETag = base64.StdEncoding.EncodeToString(sha512)
		f := BackendFile{FileInfo: info}
		f.Backend, _ = r.getBackend(backendName)
		res = append(res, f)
	}

	return res, rows.Err()
}

func (r *FileRepo) ListDeletedFilesForCleanup() ([]BackendFile, error) {
	rows, err := r.db.Query(
		"SELECT file_id, external_id, backend, name FROM files WHERE backend != 'postgres' AND deleted_at IS NOT NULL",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []BackendFile
	for rows.Next() {
		var info backend.FileInfo
		var backendName string
		var externalID *string
		if rows.Scan(&info.ID, &externalID, &backendName, &info.Name); err != nil {
			return nil, err
		}
		if externalID != nil {
			info.ExternalID = *externalID
		}
		f := BackendFile{FileInfo: info}
		f.Backend, _ = r.getBackend(backendName)
		res = append(res, f)
	}

	return res, rows.Err()
}

// Get is like Open, except the FileStream is not populated (useful for HEAD requests)
func (r *FileRepo) Get(name string, body bool) (*backend.File, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}

	var info backend.FileInfo
	var backendName string
	var externalID *string
	var sha512 []byte
	if err := tx.QueryRow(
		"SELECT file_id, file_oid, external_id, backend, name, type, size, sha512, updated_at FROM files WHERE name = $1 AND deleted_at IS NULL",
		name,
	).Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Name, &info.Type, &info.Size, &sha512, &info.ModTime); err != nil {
		if err == pgx.ErrNoRows {
			err = backend.ErrNotFound
		}
		tx.Rollback()
		return nil, err
	}
	if externalID != nil {
		info.ExternalID = *externalID
	}
	info.ETag = base64.StdEncoding.EncodeToString(sha512)
	if !body {
		tx.Rollback()
		return &backend.File{FileInfo: info, FileStream: fakeSizeSeekerFileStream{info.Size}}, nil
	}

	b, err := r.getBackend(backendName)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	stream, err := b.Open(tx, info, true)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &backend.File{FileInfo: info, FileStream: stream}, nil
}

func (r *FileRepo) Copy(to, from string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	info := backend.FileInfo{
		Name: from,
	}
	var backendName string
	var externalID *string
	var sha512, sha512State []byte
	if err := tx.QueryRow(
		"SELECT file_id, file_oid, external_id, backend, type, size, sha512, sha512_state, updated_at FROM files WHERE name = $1 AND deleted_at IS NULL", from,
	).Scan(&info.ID, &info.Oid, &externalID, &backendName, &info.Type, &info.Size, &sha512, &sha512State, &info.ModTime); err != nil {
		if err == pgx.ErrNoRows {
			return backend.ErrNotFound
		}
		tx.Rollback()
		return err
	}
	if externalID != nil {
		info.ExternalID = *externalID
	}
	b, err := r.getBackend(backendName)
	if err != nil {
		tx.Rollback()
		return err
	}

	toInfo := backend.FileInfo{
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

	if err := b.Copy(tx, toInfo, info); err != nil {
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

	info := backend.FileInfo{
		Name: name,
		Type: typ,
	}
	h := sha512.New().(resumable.Hash)
	b := r.defaultBackend

create:
	err = tx.QueryRow("INSERT INTO files (name, backend, type) VALUES ($1, $2, $3) RETURNING file_id", name, b.Name(), typ).Scan(&info.ID)
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
			b, err = r.getBackend(backendName)
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
				f, err := b.Open(tx, info, false)
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
			var di backend.FileInfo
			var externalID *string
			var backendName string
			if err := tx.QueryRow(
				"UPDATE files SET deleted_at = now() WHERE name = $1 AND deleted_at IS NULL RETURNING file_id, external_id, backend", name,
			).Scan(&di.ID, &externalID, &backendName); err != nil {
				tx.Rollback()
				return err
			}
			if externalID != nil {
				di.ExternalID = *externalID
			}
			// delete old file from backend
			if err := func() error {
				if backendName == "postgres" {
					// no need to call delete, it is done automatically by a trigger
					return nil
				}
				b, err := r.getBackend(backendName)
				if err != nil {
					return err
				}
				return b.Delete(nil, di)
			}(); err != nil {
				log.Printf("Error deleting %s (%s) from backend %s: %s", di.ExternalID, name, backendName, err)
			}
			goto create
		}
	} else if err != nil {
		tx.Rollback()
		return err
	}

	sr := newSizeReader(data)
	sr.size = info.Size
	if err := b.Put(tx, info, io.TeeReader(sr, h), offset > 0); err != nil {
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
	backendFiles := make(map[string][]backend.FileInfo)
	for rows.Next() {
		var info backend.FileInfo
		var backendName string
		var externalID *string
		if err := rows.Scan(&info.ID, &externalID, &backendName, &info.Name); err != nil {
			rows.Close()
			tx.Rollback()
			return err
		}
		if externalID != nil {
			info.ExternalID = *externalID
		}
		backendFiles[backendName] = append(backendFiles[backendName], info)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	var errors []error
	for name, files := range backendFiles {
		if name == "postgres" {
			// no need to call delete, it is done automatically by a trigger
			continue
		}
		b, err := r.getBackend(name)
		if err != nil {
			errors = append(errors, err)
		}
		for _, f := range files {
			if err := b.Delete(nil, f); err != nil {
				errors = append(errors, err)
			}
		}
	}
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}

func (f *FileRepo) SetBackend(tx *postgres.DBTx, id, name string) error {
	return tx.Exec("UPDATE files SET backend = $2 WHERE file_id = $1", id, name)
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
	if offset == 0 && whence == io.SeekEnd {
		return f.size, nil
	}
	return 0, nil
}
