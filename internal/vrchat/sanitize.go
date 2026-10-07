package vrchat

import "strings"

// SanitizeITXt cleans raw iTXt text: it drops leading NULs some exporters
// prepend, then a UTF-8 BOM, then trims surrounding whitespace, as the
// original Python did.
//
// It deliberately does NOT attempt to repair mis-encoded text. Real stored
// chunks contain double-encoded UTF-8 in display names ("Night" followed by
// U+00E2 U+02C6 U+2014, which was once U+2217); those characters are part of
// the tag value and "fixing" them here would change every tag derived from
// them.
//
// strings.TrimSpace strips exactly the set .NET's string.Trim() does (Unicode
// White_Space), so the two agree on every input.
func SanitizeITXt(s string) string {
	t := strings.TrimLeft(s, "\x00")
	t = strings.TrimLeft(t, "\uFEFF")
	return strings.TrimSpace(t)
}
