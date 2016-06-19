package installer

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	_ "github.com/cznic/ql/driver"
	"github.com/flynn/flynn/cli/config"
	"github.com/flynn/flynn/pkg/sshkeygen"
)

var keysDir, dbPath string

func init() {
	dir := filepath.Join(config.Dir(), "installer")
	keysDir = filepath.Join(dir, "keys")
	dbPath = filepath.Join(dir, "data.db")
}

func (i *Installer) openDB() error {
	i.dbMtx.Lock()
	defer i.dbMtx.Unlock()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}
	db, err := sql.Open("ql", dbPath)
	if err != nil {
		return err
	}
	i.db = db
	if err := db.Ping(); err != nil {
		return err
	}
	return i.migrateDB()
}

func saveSSHKey(name string, key *sshkeygen.SSHKey) error {
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(keysDir, name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key.PrivateKey)}); err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(keysDir, fmt.Sprintf("%s.pub", name)), key.PublicKey, 0644); err != nil {
		return err
	}
	return nil
}

func loadSSHKey(name string) (*sshkeygen.SSHKey, error) {
	key := &sshkeygen.SSHKey{}
	data, err := ioutil.ReadFile(filepath.Join(keysDir, name))
	if err != nil {
		return nil, err
	}
	b, _ := pem.Decode(data)
	key.PrivateKey, err = x509.ParsePKCS1PrivateKey(b.Bytes)
	if err != nil {
		return nil, err
	}

	key.PublicKey, err = ioutil.ReadFile(filepath.Join(keysDir, name+".pub"))
	if err != nil {
		return nil, err
	}
	return key, nil
}

func listSSHKeyNames() []string {
	names := []string{}
	filepath.Walk(keysDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == "" {
			names = append(names, path.Base(p))
		}
		return nil
	})
	return names
}
