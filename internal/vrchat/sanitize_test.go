package vrchat

import "testing"

func TestSanitizeITXt(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"surrounding whitespace", "  hello  ", "hello"},
		{"leading NULs", "\x00\x00{\"a\":1}", "{\"a\":1}"},
		{"BOM", bom + "<x/>", "<x/>"},
		{"NUL then BOM then whitespace", "\x00" + bom + "  spaced  ", "spaced"},
		// Real stored display names are double-encoded UTF-8. Those bytes are
		// part of the tag value; repairing them here would change every tag
		// derived from them and break parity.
		{"mojibake left alone", "Night\xc3\xa2\xcb\x86\xe2\x80\x94", "Night\xc3\xa2\xcb\x86\xe2\x80\x94"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeITXt(tt.in); got != tt.want {
				t.Errorf("SanitizeITXt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
