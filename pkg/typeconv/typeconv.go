package typeconv

func IntPtr(i int) *int          { return &i }
func Int64Ptr(i int64) *int64    { return &i }
func StringPtr(s string) *string { return &s }
