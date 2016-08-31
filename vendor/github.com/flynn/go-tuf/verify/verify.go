package verify

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/flynn/go-tuf/data"
	"github.com/tent/canonical-json-go"
)

type signedMeta struct {
	Type    string    `json:"_type"`
	Expires time.Time `json:"expires"`
	Version int       `json:"version"`
}

func (db *DB) Verify(s *data.Signed, role string, minVersion int) error {
	if err := db.VerifySignatures(s, role); err != nil {
		return err
	}

	sm := &signedMeta{}
	if err := json.Unmarshal(s.Signed, sm); err != nil {
		return err
	}
	if strings.ToLower(sm.Type) != strings.ToLower(role) {
		return ErrWrongMetaType
	}
	if IsExpired(sm.Expires) {
		return ErrExpired{sm.Expires}
	}
	if sm.Version < minVersion {
		return ErrLowVersion{sm.Version, minVersion}
	}

	return nil
}

var IsExpired = func(t time.Time) bool {
	return t.Sub(time.Now()) <= 0
}

func (db *DB) VerifySignatures(s *data.Signed, role string) error {
	if len(s.Signatures) == 0 {
		return ErrNoSignatures
	}

	roleData := db.GetRole(role)
	if roleData == nil {
		return ErrUnknownRole
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(s.Signed, &decoded); err != nil {
		return err
	}
	msg, err := cjson.Marshal(decoded)
	if err != nil {
		return err
	}

	valid := make(map[string]struct{})
	for _, sig := range s.Signatures {
		if !roleData.ValidKey(sig.KeyID) {
			continue
		}
		key := db.GetKey(sig.KeyID)
		if key == nil {
			continue
		}

		if err := Verifiers[key.Type].Verify(key.Value.Public, msg, sig.Signature); err != nil {
			return err
		}
		valid[sig.KeyID] = struct{}{}
	}
	if len(valid) < roleData.Threshold {
		return ErrRoleThreshold
	}

	return nil
}

func Unmarshal(b []byte, v interface{}, role string, minVersion int, db *DB) error {
	s := &data.Signed{}
	if err := json.Unmarshal(b, s); err != nil {
		return err
	}
	if err := db.Verify(s, role, minVersion); err != nil {
		return err
	}
	return json.Unmarshal(s.Signed, v)
}

func UnmarshalTrusted(b []byte, v interface{}, role string, db *DB) error {
	s := &data.Signed{}
	if err := json.Unmarshal(b, s); err != nil {
		return err
	}
	if err := db.VerifySignatures(s, role); err != nil {
		return err
	}
	return json.Unmarshal(s.Signed, v)
}
