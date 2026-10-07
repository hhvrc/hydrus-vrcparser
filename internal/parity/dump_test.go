package parity

import (
	"testing"
	"time"
)

func TestJSONWriterEscapesLikeRelaxedSystemTextJSON(t *testing.T) {
	tests := []struct{ in, want string }{
		{`plain <&'>+`, `"plain <&'>+"`},
		{"q\"b\\", `"q\"b\\"`},
		{"\t\n\r\b\f", `"\t\n\r\b\f"`},
		{"\x01\x7f", `"\u0001\u007F"`},
		{"caf\xc3\xa9 \xe2\x98\x85", "\"caf\xc3\xa9 \xe2\x98\x85\""},
		// Format characters and line separators are escaped, non-BMP code
		// points as a UTF-16 surrogate pair.
		{"\xe2\x80\x8b\xe2\x80\xa8", `"\u200B\u2028"`},
		{"\xf0\x9f\x98\x80", `"\uD83D\uDE00"`},
	}
	for _, tt := range tests {
		var w jsonWriter
		w.str(tt.in)
		if got := w.sb.String(); got != tt.want {
			t.Errorf("str(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestFormatCreated(t *testing.T) {
	plus2 := time.FixedZone("", 2*3600)
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2025, 8, 30, 6, 45, 33, 0, plus2), "2025-08-30T06:45:33+02:00"},
		{time.Date(2025, 8, 30, 6, 45, 33, 123456700, time.FixedZone("", -5*3600)), "2025-08-30T06:45:33.123456-05:00"},
		{time.Date(2025, 8, 30, 6, 45, 33, 100, time.UTC), "2025-08-30T06:45:33.000000+00:00"},
	}
	for _, tt := range tests {
		if got := formatCreated(&tt.in); got == nil || *got != tt.want {
			t.Errorf("formatCreated(%v) = %v, want %q", tt.in, got, tt.want)
		}
	}
	if formatCreated(nil) != nil {
		t.Error("nil should format as null")
	}
}
