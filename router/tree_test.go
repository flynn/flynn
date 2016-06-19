package main

import (
	"github.com/flynn/flynn/router/types"
	. "github.com/flynn/go-check"
)

func (s *S) TestTreeSlice(c *C) {
	cases := []struct {
		key     string
		parts   []string
		indices []int
	}{
		{"", []string{""}, []int{-1}},
		{"/", []string{"", ""}, []int{1, -1}},
		{"a", []string{"a"}, []int{-1}},
		{"/a/b", []string{"", "a", "b"}, []int{1, 3, -1}},
		{"a/b", []string{"a", "b"}, []int{2, -1}},
		{"/a/b/", []string{"", "a", "b", ""}, []int{1, 3, 5, -1}},
		{"a/b/", []string{"a", "b", ""}, []int{2, 4, -1}},
		{"//", []string{"", "", ""}, []int{1, 2, -1}},
		{"/a/b/c", []string{"", "a", "b", "c"}, []int{1, 3, 5, -1}},
	}

	for _, tc := range cases {
		partNum := 0
		for prefix, i := slice(tc.key, 0); ; prefix, i = slice(tc.key, i) {
			c.Assert(prefix, Equals, tc.parts[partNum])
			c.Assert(i, Equals, tc.indices[partNum])
			partNum++
			if i == -1 {
				break
			}
		}
		c.Assert(partNum, Equals, len(tc.parts))
	}
}

func (s *S) TestTree(c *C) {
	root := NewTree(&httpRoute{HTTPRoute: &router.HTTPRoute{ID: "/"}})
	root.Insert("/a/", &httpRoute{HTTPRoute: &router.HTTPRoute{ID: "/a/"}})
	root.Insert("/a/b/", &httpRoute{HTTPRoute: &router.HTTPRoute{ID: "/a/b/"}})
	root.Insert("/c/", &httpRoute{HTTPRoute: &router.HTTPRoute{ID: "/c/"}})
	root.Insert("/a/c/d/c/", &httpRoute{HTTPRoute: &router.HTTPRoute{ID: "/c/"}})
	// the root should return the default backend
	c.Assert(root.Lookup("/").HTTPRoute.ID, Equals, "/")
	// unmatched should hit default route
	c.Assert(root.Lookup("/xyz").HTTPRoute.ID, Equals, "/")
	// exact route match
	c.Assert(root.Lookup("/a/").HTTPRoute.ID, Equals, "/a/")
	// trailing bytes
	c.Assert(root.Lookup("/a/xyz").HTTPRoute.ID, Equals, "/a/")
	// nested exact route match
	c.Assert(root.Lookup("/a/b/").HTTPRoute.ID, Equals, "/a/b/")
	// leaf nodes should properly be ignored
	c.Assert(root.Lookup("/a/c/d").HTTPRoute.ID, Equals, "/a/")
	// nested trailing bytes
	c.Assert(root.Lookup("/a/b/xyz").HTTPRoute.ID, Equals, "/a/b/")
	// route should work before removal
	c.Assert(root.Lookup("/c/").HTTPRoute.ID, Equals, "/c/")
	root.Remove("/c")
	// but not afterwards
	c.Assert(root.Lookup("/c/").HTTPRoute.ID, Equals, "/")
}
