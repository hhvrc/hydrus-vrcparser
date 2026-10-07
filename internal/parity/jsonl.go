// Package parity dumps the VRChat parser's results over a real database as
// JSONL, so tools/parity/compare_*.py can diff a dump taken before a change
// against one taken after it. The format is fixed: it is byte-compatible with
// the dumps the parsers were originally verified against.
package parity

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// The dumps are compact JSON, escaped the way .NET's System.Text.Json with
// JavaScriptEncoder.UnsafeRelaxedJsonEscaping does it (the original dumpers
// used that). These helpers write those bytes for the values the dumps hold:
// object keys in the given order, strings, ints, null and string arrays.
//
// The relaxed encoder escapes only '"', '\\', and the code points it does not
// consider safe: controls (as \b \t \n \f \r or \u00XX), anything outside the
// L/M/N/P/S/Zs categories (format, private-use, unassigned, line/paragraph
// separators), and every non-BMP code point, written as an escaped UTF-16
// surrogate pair. Hex digits are uppercase.

type jsonWriter struct {
	sb strings.Builder
}

func (w *jsonWriter) raw(s string) { w.sb.WriteString(s) }

func (w *jsonWriter) str(s string) {
	w.sb.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			w.sb.WriteString(`\"`)
		case r == '\\':
			w.sb.WriteString(`\\`)
		case r == '\b':
			w.sb.WriteString(`\b`)
		case r == '\t':
			w.sb.WriteString(`\t`)
		case r == '\n':
			w.sb.WriteString(`\n`)
		case r == '\f':
			w.sb.WriteString(`\f`)
		case r == '\r':
			w.sb.WriteString(`\r`)
		case r < 0x80 && r >= 0x20 && r != 0x7F:
			w.sb.WriteRune(r)
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&w.sb, `\u%04X\u%04X`, hi, lo)
		case r == utf8.RuneError || isRelaxedSafe(r):
			w.sb.WriteRune(r)
		default:
			fmt.Fprintf(&w.sb, `\u%04X`, r)
		}
	}
	w.sb.WriteByte('"')
}

func isRelaxedSafe(r rune) bool {
	return unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Zs)
}

func (w *jsonWriter) strOrNull(s *string) {
	if s == nil {
		w.raw("null")
		return
	}
	w.str(*s)
}

func (w *jsonWriter) strArray(items []string) {
	w.sb.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			w.sb.WriteByte(',')
		}
		w.str(s)
	}
	w.sb.WriteByte(']')
}

// key writes a property name, preceded by a comma unless it is the first.
func (w *jsonWriter) key(name string, first bool) {
	if !first {
		w.sb.WriteByte(',')
	}
	w.str(name)
	w.sb.WriteByte(':')
}
