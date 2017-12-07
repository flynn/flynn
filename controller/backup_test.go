package main

import (
	"time"

	"github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	. "github.com/flynn/go-check"
)

func (s *S) TestGetBackup(c *C) {
	_, err := s.c.GetBackupMeta()
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, controller.ErrNotFound)

	db := s.hc.db
	now := time.Now()
	b := &ct.ClusterBackup{
		Status:      ct.ClusterBackupStatusComplete,
		SHA512:      "fake-hash",
		Size:        123,
		CompletedAt: &now,
	}
	err = db.QueryRow("backup_insert", b.Status, b.SHA512, b.Size, b.Error, b.CompletedAt).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	c.Assert(err, IsNil)

	rb, err := s.c.GetBackupMeta()
	c.Assert(err, IsNil)
	c.Assert(rb.ID, Equals, b.ID)
	c.Assert(rb.Status, Equals, b.Status)
	c.Assert(rb.SHA512, Equals, b.SHA512)
	c.Assert(rb.Size, Equals, b.Size)
	c.Assert(rb.CompletedAt, Not(IsNil))
	c.Assert(rb.CreatedAt, Not(IsNil))
	c.Assert(rb.UpdatedAt, Not(IsNil))
	c.Assert(rb.CompletedAt.Truncate(time.Millisecond).Equal(b.CompletedAt.Truncate(time.Millisecond)), Equals, true)
	c.Assert(rb.CreatedAt.Truncate(time.Millisecond).Equal(b.CreatedAt.Truncate(time.Millisecond)), Equals, true)
	c.Assert(rb.UpdatedAt.Truncate(time.Millisecond).Equal(b.UpdatedAt.Truncate(time.Millisecond)), Equals, true)
}
