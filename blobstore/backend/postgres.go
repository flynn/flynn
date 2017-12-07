package backend

import (
	"io"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/jackc/pgx"
)

var Postgres Backend = pg{}

type pg struct{}

func (pg) Name() string { return "postgres" }

func (p pg) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error {
	if !append {
		if err := tx.QueryRow("UPDATE files SET file_oid = lo_create(0) WHERE file_id = $1 RETURNING file_oid", info.ID).Scan(&info.Oid); err != nil {
			return err
		}
	}

	lo, err := tx.LargeObjects()
	if err != nil {
		return err
	}
	obj, err := lo.Open(*info.Oid, pgx.LargeObjectModeWrite)
	if err != nil {
		return err
	}
	if append {
		obj.Seek(info.Size, io.SeekStart)
	}
	if _, err := io.Copy(obj, r); err != nil {
		return err
	}

	return nil
}

func (p pg) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	srcFile, err := p.Open(tx, src, false)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	return p.Put(tx, dst, srcFile, false)
}

func (p pg) Delete(tx *postgres.DBTx, info FileInfo) error {
	if err := tx.Exec("SELECT lo_unlink($1)", info.Oid); err != nil {
		return err
	}
	return tx.Exec("UPDATE files SET file_oid = NULL WHERE file_id = $1", info.ID)
}

func (p pg) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if info.Oid == nil {
		return nil, ErrNotFound
	}

	lo, err := tx.LargeObjects()
	if err != nil {
		return nil, err
	}
	obj, err := lo.Open(*info.Oid, pgx.LargeObjectModeRead)
	if err != nil {
		return nil, err
	}
	f := &pgFile{LargeObject: obj, size: info.Size}
	if txControl {
		f.tx = tx
	}
	return f, nil
}

type pgFile struct {
	*pgx.LargeObject
	size     int64
	sizeRead bool
	tx       *postgres.DBTx
}

func (f *pgFile) Close() error {
	if f.tx != nil {
		f.tx.Rollback()
	}
	return nil
}

func (f *pgFile) Seek(offset int64, whence int) (int64, error) {
	// HACK: work around ServeContent length detection, remove when fixed
	if offset == 0 && whence == io.SeekEnd {
		f.sizeRead = true
		return f.size, nil
	} else if f.sizeRead && offset == 0 && whence == io.SeekStart {
		return 0, nil
	}
	return f.LargeObject.Seek(offset, whence)
}
