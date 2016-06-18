package rfc5424

import (
	"bytes"

	. "github.com/flynn/go-check"
)

func (s *S) TestStructuredData(c *C) {
	table := []struct {
		sd   *StructuredData
		want string
	}{
		{
			sd: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
			},
			want: `[asdf foo="bar"]`,
		},
		{
			sd: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
					{
						Name:  []byte("bar"),
						Value: []byte("baz"),
					},
				},
			},
			want: `[asdf foo="bar" bar="baz"]`,
		},
		{
			sd: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\"az`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`baz]"\`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\\`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\a"`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte("baz"),
					},
				},
			},
			want: `[asdf foo="bar" bar="b\\\"az" bar="baz\]\"\\" bar="b\\\\" bar="b\\a\"" bar="baz"]`,
		},
	}

	for _, test := range table {
		buf := &bytes.Buffer{}
		test.sd.Encode(buf)
		c.Assert(buf.String(), Equals, test.want)
	}
}

func (s *S) TestStructuredDataParser(c *C) {
	table := []struct {
		sd   string
		want *StructuredData
	}{
		{
			sd: `[asdf foo="bar"]`,
			want: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
				},
			},
		},
		{
			sd: `[asdf foo="bar" bar="baz"]`,
			want: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
					{
						Name:  []byte("bar"),
						Value: []byte("baz"),
					},
				},
			},
		},
		{
			sd: `[asdf foo="bar" bar="b\\\"az" bar="baz\]\"\\" bar="b\\\\" bar="b\a\"" bar="baz"]`,
			want: &StructuredData{
				ID: []byte("asdf"),
				Params: []StructuredDataParam{
					{
						Name:  []byte("foo"),
						Value: []byte("bar"),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\"az`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`baz]"\`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\\`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte(`b\a"`),
					},
					{
						Name:  []byte("bar"),
						Value: []byte("baz"),
					},
				},
			},
		},
		{
			sd: `[asdf]`,
			want: &StructuredData{
				ID:     []byte("asdf"),
				Params: []StructuredDataParam{},
			},
		},
		{sd: `[asdf  foo="bar" bar="baz"]`},
		{sd: `[asdf= foo="bar" bar="baz"]`},
		{sd: `[ foo="bar" bar="baz"]`},
		{sd: `[foo="bar" bar="baz"]`},
		{sd: `[asdf="foo" foo="bar" bar="baz"]`},
		{sd: `[foo="bar" bar="baz" ]`},
		{sd: `[foo="bar" bar="baz" ]`},
		{sd: `[foo="bar"  bar="baz"]`},
		{sd: `[foo="bar" b bar="baz"]`},
		{sd: `[]`},
		{sd: `[=]`},
		{sd: `[`},
		{sd: ``},
		{sd: `[bar=`},
		{sd: `[bar="asdf]`},
		{sd: `[bar="asdf"] `},
		{sd: `[bar "asdf"]`},
		{sd: `[ bar="asdf"]`},
	}

	for i, test := range table {
		info := Commentf("i = %d", i)
		res, err := ParseStructuredData([]byte(test.sd))
		if test.want == nil {
			c.Assert(res, IsNil, Commentf("i = %d", i))
			c.Assert(err, NotNil, Commentf("i = %d", i))
			continue
		}
		c.Assert(err, IsNil, info)
		c.Assert(res, DeepEquals, test.want, info)
	}
}
