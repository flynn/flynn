package data

import (
	"testing"
	"time"
)

// TestPageTokenCursor tests converting PageToken CursorIDs to time.Time
func TestPageTokenCursor(t *testing.T) {
	type test struct {
		name      string
		cursorID  string
		expected  time.Time
		expectErr bool
	}
	for _, x := range []test{
		{
			name:     "zero",
			cursorID: "0",
			expected: time.Unix(0, 0),
		},
		{
			name:     "just-seconds",
			cursorID: "1579014055000000",
			expected: time.Unix(1579014055, 0),
		},
		{
			name:     "with-microseconds",
			cursorID: "1579014055123456",
			expected: time.Unix(1579014055, 123456000),
		},
		{
			name:      "non-digits",
			cursorID:  "foo",
			expectErr: true,
		},
	} {
		t.Run(x.name, func(t *testing.T) {
			token := &PageToken{
				CursorID: &x.cursorID,
			}
			cursor, err := token.Cursor()
			if x.expectErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !cursor.Equal(x.expected) {
				t.Fatalf("expected cursor to equal %v, got %v", x.expected, cursor)
			}
			cursorID := toCursorID(&x.expected)
			if cursorID == nil {
				t.Fatalf("expected toCursorID to return non-nil, got nil")
			}
			if *cursorID != x.cursorID {
				t.Fatalf("expected cursorID %q, got %q", x.cursorID, *cursorID)
			}
		})
	}
}
