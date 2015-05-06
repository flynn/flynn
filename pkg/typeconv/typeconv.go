package typeconv

func IntPtr(i int) *int          { return &i }
func StringPtr(s string) *string { return &s }
