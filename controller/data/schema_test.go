package data

import (
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/pkg/postgres"
	. "github.com/flynn/go-check"
	"github.com/jackc/pgx"
)

const bufSize = 1024 * 1024

type S struct {
	db *postgres.DB
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
	dbname := "controllerschematest"
	db := setupTestDB(c, dbname)
	if err := MigrateDB(db); err != nil {
		c.Fatal(err)
	}

	// reconnect with que statements prepared now that schema is migrated
	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "/var/run/postgresql",
			Database: dbname,
		},
		AfterConnect: PrepareStatements,
	})
	if err != nil {
		c.Fatal(err)
	}
	db = postgres.New(pgxpool, nil)
	s.db = db
}

func (s *S) matchLabelFilters(c *C, labelFilters []ct.LabelFilter, labels map[string]string) bool {
	var ret bool
	c.Assert(s.db.QueryRow("SELECT match_label_filters($1, $2)", labelFilters, labels).Scan(&ret), IsNil)
	return ret
}

func (s *S) TestMatchLabelFilters(c *C) {
	labels := map[string]string{
		"one":   "ONE",
		"two":   "TWO",
		"three": "THREE",
		"four":  "FOUR",
	}

	// OP_IN
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "one",
				Values: []string{"1", "ONE"},
			},
		},
	}, labels), Equals, true)
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "one",
				Values: []string{"1"},
			},
		},
	}, labels), Equals, false)

	// OP_NOT_IN
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpNotIn,
				Key:    "one",
				Values: []string{"1", "foo", "bar"},
			},
		},
	}, labels), Equals, true)
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpNotIn,
				Key:    "one",
				Values: []string{"1", "ONE"},
			},
		},
	}, labels), Equals, false)

	// OP_EXISTS
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "one",
			},
		},
	}, labels), Equals, true)
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "foo",
			},
		},
	}, labels), Equals, false)

	// OP_NOT_EXISTS
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "foo",
			},
		},
	}, labels), Equals, true)
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "one",
			},
		},
	}, labels), Equals, false)

	// Multiple Expressions
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "one",
				Values: []string{"1", "ONE", "one"},
			},
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpNotIn,
				Key:    "one",
				Values: []string{"foo", "bar", "baz"},
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "foo",
			},
		},
	}, labels), Equals, true) // expressions are ANDed
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "one",
				Values: []string{"1", "ONE", "one"},
			},
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpNotIn,
				Key:    "one",
				Values: []string{"foo", "bar", "baz"},
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "foo",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "four", // it exists
			},
		},
	}, labels), Equals, false) // expressions are ANDed

	// Multiple Filters
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "three",
				Values: []string{"foo", "bar"},
			},
		}, // false
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "one",
				Values: []string{"1", "ONE"},
			},
		}, // true
		{
			&ct.LabelFilterExpression{
				Op:     ct.LabelFilterExpressionOpIn,
				Key:    "two",
				Values: []string{"2", "TWO"},
			},
		}, // true
	}, labels), Equals, true) // filtered are ORed

	// Multiple Filters with Multiple Expressions
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "one",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "foo",
			},
		}, // false
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "one",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
		}, // true
	}, labels), Equals, true) // filtered are ORed
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "one",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "foo",
			},
		}, // false
		{
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "one",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "two",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpExists,
				Key: "three",
			},
			&ct.LabelFilterExpression{
				Op:  ct.LabelFilterExpressionOpNotExists,
				Key: "four",
			},
		}, // false
	}, labels), Equals, false) // filtered are ORed

	// Empty List of Filters should always match
	c.Assert(s.matchLabelFilters(c, []ct.LabelFilter{}, labels), Equals, true)
}
