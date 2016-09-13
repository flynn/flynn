package typeconv

import "time"

func IntPtr(i int) *int              { return &i }
func Int32Ptr(i int32) *int32        { return &i }
func Uint32Ptr(i uint32) *uint32     { return &i }
func Int64Ptr(i int64) *int64        { return &i }
func StringPtr(s string) *string     { return &s }
func TimePtr(t time.Time) *time.Time { return &t }
func BoolPtr(b bool) *bool           { return &b }
