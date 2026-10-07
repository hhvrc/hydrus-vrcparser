package vrchat

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// isoDateTime matches the ISO 8601 shapes XMP timestamps take: a date,
// optionally followed by a time (minutes, seconds and a fraction of any
// length), optionally followed by an offset.
var isoDateTime = regexp.MustCompile(
	`^(\d{4})-(\d{2})-(\d{2})` +
		`(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?)?` +
		`(Z|[+-]\d{2}(?::?\d{2})?)?$`)

// parseXMPDateTime parses an XMP timestamp, preserving its original UTC
// offset. It returns nil for an absent, blank or unparseable value.
//
// The offset matters: the date tag is rendered from this value, so
// normalizing to UTC could shift a late-evening screenshot onto the previous
// day. Offset-less values are treated as UTC, matching _parse_dt.
//
// The C# used DateTimeOffset.TryParse, which also accepts a zoo of
// culture-style formats ("8/30/2025", "Aug 30 2025 ..."). XMP dates are ISO
// 8601 by specification, and every value in the corpus is
// yyyy-MM-ddTHH:mm:ss[.fffffff](+|-)HH:mm, so only the ISO forms are
// accepted here. Fractions are rounded to .NET's 100ns tick, as TryParse does.
func parseXMPDateTime(value *string) *time.Time {
	if value == nil {
		return nil
	}
	s := strings.TrimSpace(*value)
	if s == "" {
		return nil
	}

	m := isoDateTime.FindStringSubmatch(s)
	if m == nil {
		return nil
	}

	atoi := func(v string) int {
		n, _ := strconv.Atoi(v)
		return n
	}
	year, month, day := atoi(m[1]), atoi(m[2]), atoi(m[3])
	hour, minute, sec := atoi(m[4]), atoi(m[5]), atoi(m[6])
	if month < 1 || month > 12 || day < 1 || day > 31 ||
		hour > 23 || minute > 59 || sec > 59 || year < 1 {
		return nil
	}

	offset := 0
	if z := m[8]; z != "" && z != "Z" {
		sign := 1
		if z[0] == '-' {
			sign = -1
		}
		digits := strings.ReplaceAll(z[1:], ":", "")
		oh := atoi(digits[:2])
		om := 0
		if len(digits) == 4 {
			om = atoi(digits[2:])
		}
		if om > 59 || oh*60+om > 14*60 {
			return nil
		}
		offset = sign * (oh*3600 + om*60)
	}

	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.FixedZone("", offset))
	if t.Day() != day {
		// time.Date normalized an impossible date such as February 30.
		return nil
	}

	if frac := m[7]; frac != "" {
		f, err := strconv.ParseFloat("0."+frac, 64)
		if err != nil {
			return nil
		}
		ticks := int64(math.Round(f * 1e7))
		t = t.Add(time.Duration(ticks * 100))
	}
	return &t
}
