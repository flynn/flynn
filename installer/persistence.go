package installer

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"

	"github.com/flynn/flynn/pkg/sshkeygen"
)

var keysDir, dataPath string

func init() {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	dir := filepath.Join(u.HomeDir, ".flynn-installer")
	keysDir = filepath.Join(dir, "keys")
	dataPath = filepath.Join(dir, "data.json")
}

func (s *Stack) load() error {
	s.persistMutex.Lock()
	defer s.persistMutex.Unlock()

	file, err := os.Open(dataPath)
	if err != nil {
		return err
	}
	defer file.Close()
	dec := json.NewDecoder(file)
	if err := dec.Decode(&s); err != nil {
		return err
	}
	return nil
}

func (s *Stack) persist() error {
	s.persistMutex.Lock()
	defer s.persistMutex.Unlock()

	if err := os.MkdirAll(filepath.Dir(dataPath), 0755); err != nil {
		return err
	}
	file, err := os.Create(dataPath)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := json.NewEncoder(file)
	if err := enc.Encode(s); err != nil {
		return err
	}
	return nil
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
