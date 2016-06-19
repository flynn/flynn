package main

import . "github.com/flynn/go-check"

func (S) TestParseTagArgs(c *C) {
	type test struct {
		args     string
		expected map[string]string
	}
	for _, t := range []*test{
		{
			args:     "",
			expected: map[string]string{},
		},
		{
			args:     "foo",
			expected: map[string]string{"foo": "true"},
		},
		{
			args:     "foo,",
			expected: map[string]string{"foo": "true"},
		},
		{
			args:     "foo=",
			expected: map[string]string{"foo": ""},
		},
		{
			args:     "foo=bar",
			expected: map[string]string{"foo": "bar"},
		},
		{
			args:     "foo=bar,",
			expected: map[string]string{"foo": "bar"},
		},
		{
			args:     "foo=bar=baz",
			expected: map[string]string{"foo": "bar=baz"},
		},
		{
			args:     "foo=bar,baz",
			expected: map[string]string{"foo": "bar", "baz": "true"},
		},
		{
			args:     "foo=bar,baz=",
			expected: map[string]string{"foo": "bar", "baz": ""},
		},
		{
			args:     "foo=bar,baz=,",
			expected: map[string]string{"foo": "bar", "baz": ""},
		},
		{
			args:     "foo=bar,baz=qux",
			expected: map[string]string{"foo": "bar", "baz": "qux"},
		},
	} {
		actual := parseTagArgs(t.args)
		c.Assert(actual, DeepEquals, t.expected, Commentf("parsing %q", t.args))
	}
}
