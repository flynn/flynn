package snapshot

import (
	"fmt"
	"io"
	"testing"

	"github.com/flynn/flynn/pkg/syslog/rfc5424"

	. "github.com/flynn/flynn/Godeps/_workspace/src/github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapshotTestSuite struct{}

var _ = Suite(&SnapshotTestSuite{})

func (_ *SnapshotTestSuite) TestRoundtrip(c *C) {
	want := []*rfc5424.Message{}
	got := []*rfc5424.Message{}

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("app-%d", i)
		hdr := rfc5424.Header{AppName: []byte(key)}

		for j := 0; j < 100; j++ {
			line := []byte(fmt.Sprintf("line %d\n", j))
			want = append(want, rfc5424.NewMessage(&hdr, line))
		}
	}

	r, w := io.Pipe()
	errc := make(chan error)
	go func() {
		err := WriteTo([][]*rfc5424.Message{want}, w)
		w.Close()
		errc <- err

	}()

	s := NewScanner(r)
	for s.Scan() {
		got = append(got, s.Message)
	}
	c.Assert(s.Err(), IsNil)

	c.Assert(<-errc, IsNil)
	c.Assert(want, DeepEquals, got)
}
