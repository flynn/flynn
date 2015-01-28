package client

import (
	"encoding/json"
	"path/filepath"

	. "gopkg.in/check.v1"
)

type LocalStoreSuite struct{}

var _ = Suite(&LocalStoreSuite{})

func (LocalStoreSuite) TestFileLocalStore(c *C) {
	tmp := c.MkDir()
	path := filepath.Join(tmp, "tuf.db")
	store, err := FileLocalStore(path)
	c.Assert(err, IsNil)

	type meta map[string]json.RawMessage

	assertGet := func(expected meta) {
		actual, err := store.GetMeta()
		c.Assert(err, IsNil)
		c.Assert(meta(actual), DeepEquals, expected)
	}

	// initial GetMeta should return empty meta
	assertGet(meta{})

	// SetMeta should persist
	rootJSON := []byte(`{"_type":"Root"}`)
	c.Assert(store.SetMeta("root.json", rootJSON), IsNil)
	assertGet(meta{"root.json": rootJSON})

	// SetMeta should add to existing meta
	targetsJSON := []byte(`{"_type":"Target"}`)
	c.Assert(store.SetMeta("targets.json", targetsJSON), IsNil)
	assertGet(meta{"root.json": rootJSON, "targets.json": targetsJSON})

	// a new store should get the same meta
	c.Assert(store.(*fileLocalStore).db.Close(), IsNil)
	store, err = FileLocalStore(path)
	c.Assert(err, IsNil)
	assertGet(meta{"root.json": rootJSON, "targets.json": targetsJSON})
}
