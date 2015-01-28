package util

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-tuf/data"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type UtilSuite struct{}

var _ = Suite(&UtilSuite{})

func (UtilSuite) TestGenerateFileMetaDefault(c *C) {
	// default is sha512
	r := bytes.NewReader([]byte("foo"))
	meta, err := GenerateFileMeta(r)
	c.Assert(err, IsNil)
	c.Assert(meta.Length, Equals, int64(3))
	hashes := meta.Hashes
	c.Assert(hashes, HasLen, 1)
	hash, ok := hashes["sha512"]
	if !ok {
		c.Fatal("missing sha512 hash")
	}
	c.Assert(hash.String(), DeepEquals, "f7fbba6e0636f890e56fbbf3283e524c6fa3204ae298382d624741d0dc6638326e282c41be5e4254d8820772c5518a2c5a8c0c7f7eda19594a7eb539453e1ed7")
}

func (UtilSuite) TestGenerateFileMetaExplicit(c *C) {
	r := bytes.NewReader([]byte("foo"))
	meta, err := GenerateFileMeta(r, "sha256", "sha512")
	c.Assert(err, IsNil)
	c.Assert(meta.Length, Equals, int64(3))
	hashes := meta.Hashes
	c.Assert(hashes, HasLen, 2)
	for name, val := range map[string]string{
		"sha256": "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		"sha512": "f7fbba6e0636f890e56fbbf3283e524c6fa3204ae298382d624741d0dc6638326e282c41be5e4254d8820772c5518a2c5a8c0c7f7eda19594a7eb539453e1ed7",
	} {
		hash, ok := hashes[name]
		if !ok {
			c.Fatalf("missing %s hash", name)
		}
		c.Assert(hash.String(), DeepEquals, val)
	}
}

func (UtilSuite) TestFileMetaEqual(c *C) {
	type test struct {
		name string
		b    data.FileMeta
		a    data.FileMeta
		err  func(test) error
	}
	fileMeta := func(length int64, hashes map[string]string) data.FileMeta {
		m := data.FileMeta{Length: length, Hashes: make(map[string]data.HexBytes, len(hashes))}
		for typ, hash := range hashes {
			v, err := hex.DecodeString(hash)
			c.Assert(err, IsNil)
			m.Hashes[typ] = v
		}
		return m
	}
	tests := []test{
		{
			name: "wrong length",
			a:    data.FileMeta{Length: 1},
			b:    data.FileMeta{Length: 2},
			err:  func(test) error { return ErrWrongLength },
		},
		{
			name: "wrong sha512 hash",
			a:    fileMeta(10, map[string]string{"sha512": "111111"}),
			b:    fileMeta(10, map[string]string{"sha512": "222222"}),
			err:  func(t test) error { return ErrWrongHash{"sha512", t.b.Hashes["sha512"], t.a.Hashes["sha512"]} },
		},
		{
			name: "intersecting hashes",
			a:    fileMeta(10, map[string]string{"sha512": "111111", "md5": "222222"}),
			b:    fileMeta(10, map[string]string{"sha512": "111111", "sha256": "333333"}),
			err:  func(test) error { return nil },
		},
		{
			name: "no common hashes",
			a:    fileMeta(10, map[string]string{"sha512": "111111"}),
			b:    fileMeta(10, map[string]string{"sha256": "222222", "md5": "333333"}),
			err:  func(t test) error { return ErrNoCommonHash{t.b.Hashes, t.a.Hashes} },
		},
	}
	for _, t := range tests {
		c.Assert(FileMetaEqual(t.a, t.b), DeepEquals, t.err(t), Commentf("name = %s", t.name))
	}
}

func (UtilSuite) TestNormalizeTarget(c *C) {
	for before, after := range map[string]string{
		"":                    "/",
		"foo.txt":             "/foo.txt",
		"/bar.txt":            "/bar.txt",
		"foo//bar.txt":        "/foo/bar.txt",
		"/with/./a/dot":       "/with/a/dot",
		"/with/double/../dot": "/with/dot",
	} {
		c.Assert(NormalizeTarget(before), Equals, after)
	}
}

func (UtilSuite) TestHashedPaths(c *C) {
	hexBytes := func(s string) data.HexBytes {
		v, err := hex.DecodeString(s)
		c.Assert(err, IsNil)
		return v
	}
	hashes := data.Hashes{
		"sha512": hexBytes("abc123"),
		"sha256": hexBytes("def456"),
	}
	paths := HashedPaths("foo/bar.txt", hashes)
	// cannot use DeepEquals as the returned order is non-deterministic
	c.Assert(paths, HasLen, 2)
	expected := map[string]struct{}{"foo/abc123.bar.txt": {}, "foo/def456.bar.txt": {}}
	for _, path := range paths {
		if _, ok := expected[path]; !ok {
			c.Fatalf("unexpected path: %s", path)
		}
		delete(expected, path)
	}
}
