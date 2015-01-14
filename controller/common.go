package main

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-sql"
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/pq/hstore"
)

func metaToHstore(m map[string]string) hstore.Hstore {
	var s hstore.Hstore
	if len(m) > 0 {
		s.Map = make(map[string]sql.NullString, len(m))
		for k, v := range m {
			s.Map[k] = sql.NullString{String: v, Valid: true}
		}
	}
	return s
}
