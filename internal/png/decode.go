package png

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8 converts bytes to a string as UTF-8, replacing each ill-formed
// sequence with U+FFFD.
//
// The replacement granularity matters: stored chunk text, and the tags derived
// from it, came from Python's errors="replace" and .NET's Encoding.UTF8, which
// both follow the Unicode "maximal subpart" practice -- one U+FFFD per maximal
// prefix of a valid sequence, or per lone byte. Go's strings.ToValidUTF8
// collapses whole runs into one U+FFFD, and utf8.DecodeRune emits one per
// byte, so neither matches.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}

	var sb strings.Builder
	sb.Grow(len(b) + 8)
	for i := 0; i < len(b); {
		c := b[i]
		if c < utf8.RuneSelf {
			sb.WriteByte(c)
			i++
			continue
		}

		need, lo, hi := leadInfo(c)
		if need == 0 {
			// Continuation byte or a byte that never starts a sequence.
			sb.WriteRune(utf8.RuneError)
			i++
			continue
		}

		// Consume as many bytes as continue a valid sequence.
		j := i + 1
		for k := 0; k < need && j < len(b); k++ {
			cb := b[j]
			if k == 0 {
				if cb < lo || cb > hi {
					break
				}
			} else if cb < 0x80 || cb > 0xBF {
				break
			}
			j++
		}

		if j-i == need+1 {
			sb.Write(b[i:j])
		} else {
			sb.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return sb.String()
}

// leadInfo returns how many continuation bytes a lead byte needs and the valid
// range of the first one (which excludes overlongs and surrogates). need is 0
// for bytes that cannot start a sequence.
func leadInfo(c byte) (need int, lo, hi byte) {
	switch {
	case c >= 0xC2 && c <= 0xDF:
		return 1, 0x80, 0xBF
	case c == 0xE0:
		return 2, 0xA0, 0xBF
	case c == 0xED:
		return 2, 0x80, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		return 2, 0x80, 0xBF
	case c == 0xF0:
		return 3, 0x90, 0xBF
	case c >= 0xF1 && c <= 0xF3:
		return 3, 0x80, 0xBF
	case c == 0xF4:
		return 3, 0x80, 0x8F
	default:
		return 0, 0, 0
	}
}
