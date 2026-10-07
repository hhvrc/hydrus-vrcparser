package vrchat

import (
	"math"
	"strconv"
	"strings"
)

// The C# parsed numbers with int.TryParse / double.TryParse under the
// invariant culture. These helpers reproduce the accept/reject decisions of
// those calls, which is what matters here: a rejected player coordinate drops
// the player, a rejected index drops the whole line. strconv alone is both
// stricter (no surrounding whitespace) and looser (hex floats, underscores
// with prefixes) than .NET.

// trimDotnetNumberWhite strips what .NET's number parser tolerates around a
// number: ASCII whitespace (U+0009..U+000D, U+0020) on both sides, plus
// trailing NULs, which .NET silently ignores.
func trimDotnetNumberWhite(s string) string {
	s = strings.TrimRight(s, "\x00")
	return strings.Trim(s, "\t\n\v\f\r ")
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseDotnetInt32 mirrors int.TryParse(s, NumberStyles.Integer,
// CultureInfo.InvariantCulture): optional surrounding whitespace, an optional
// leading sign, ASCII digits, and the 32-bit range.
func parseDotnetInt32(s string) (int, bool) {
	s = trimDotnetNumberWhite(s)
	digits := s
	if len(digits) > 0 && (digits[0] == '+' || digits[0] == '-') {
		digits = digits[1:]
	}
	if digits == "" {
		return 0, false
	}
	for i := 0; i < len(digits); i++ {
		if !isASCIIDigit(digits[i]) {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(n), true
}

// parseDotnetDouble mirrors double.TryParse under NumberStyles.Float (plus
// AllowThousands when allowThousands is set) and the invariant culture.
//
// Accepted: surrounding whitespace, a leading sign, digits with an optional
// decimal point, an optional exponent, and the invariant specials "Infinity",
// "NaN" and the infinity sign, case-insensitively. Overflow yields an infinity
// and still succeeds, as it has since .NET Core 3.0.
func parseDotnetDouble(s string, allowThousands bool) (float64, bool) {
	s = trimDotnetNumberWhite(s)

	body := s
	neg := false
	if len(body) > 0 && (body[0] == '+' || body[0] == '-') {
		neg = body[0] == '-'
		body = body[1:]
	}

	switch {
	case strings.EqualFold(body, "Infinity"), body == "\u221E":
		if neg {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case strings.EqualFold(body, "NaN"):
		return math.NaN(), true
	}

	var clean strings.Builder
	clean.Grow(len(s))
	if neg {
		clean.WriteByte('-')
	}

	i := 0
	intDigits := 0
	for i < len(body) && (isASCIIDigit(body[i]) || (allowThousands && body[i] == ',' && intDigits > 0)) {
		if body[i] != ',' {
			clean.WriteByte(body[i])
			intDigits++
		}
		i++
	}

	fracDigits := 0
	if i < len(body) && body[i] == '.' {
		clean.WriteByte('.')
		i++
		for i < len(body) && isASCIIDigit(body[i]) {
			clean.WriteByte(body[i])
			fracDigits++
			i++
		}
	}
	if intDigits+fracDigits == 0 {
		return 0, false
	}

	if i < len(body) && (body[i] == 'e' || body[i] == 'E') {
		clean.WriteByte('e')
		i++
		if i < len(body) && (body[i] == '+' || body[i] == '-') {
			clean.WriteByte(body[i])
			i++
		}
		expDigits := 0
		for i < len(body) && isASCIIDigit(body[i]) {
			clean.WriteByte(body[i])
			expDigits++
			i++
		}
		if expDigits == 0 {
			return 0, false
		}
	}

	if i != len(body) {
		return 0, false
	}

	f, err := strconv.ParseFloat(clean.String(), 64)
	if err != nil {
		// Only a range error is possible after the validation above, and
		// ParseFloat has already produced the signed infinity (or zero) .NET
		// would.
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return 0, false
		}
	}
	return f, true
}

// truncToInt mirrors C#'s (int)Math.Truncate(d), which saturates on
// out-of-range values since .NET 9.
func truncToInt(d float64) int {
	t := math.Trunc(d)
	switch {
	case math.IsNaN(t):
		return 0
	case t >= math.MaxInt32:
		return math.MaxInt32
	case t <= math.MinInt32:
		return math.MinInt32
	}
	return int(t)
}
