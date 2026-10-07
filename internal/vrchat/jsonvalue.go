package vrchat

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// JSON payloads are decoded into a generic tree: map[string]any, []any,
// string, json.Number, bool and nil. Numbers stay json.Number so the
// normalizer can apply System.Text.Json's TryGetInt32 / TryGetDouble rules to
// the literal.
//
// Behaviour against System.Text.Json's JsonDocument, which the C# used:
//
//   - The grammar is the same strict RFC 8259 (no comments, no trailing
//     commas, no NaN/Infinity, nothing after the root). Python's json.loads
//     accepted NaN/Infinity; the C# did not, and neither does this.
//   - Duplicate keys: the last one wins, as JsonElement.TryGetProperty (which
//     searches backwards) and Python's dict both did.
//   - Nesting deeper than 64 is rejected, JsonDocument's default MaxDepth.
//   - A lone surrogate escape such as "\ud800" decodes to U+FFFD here.
//     Python kept the surrogate; .NET's GetString threw. The corpus has none.

// maxJSONDepth is JsonDocument's default JsonReaderOptions.MaxDepth.
const maxJSONDepth = 64

// JSONError reports a payload that is not valid JSON. The parity dump records
// it as Python's "JSONDecodeError".
type JSONError struct {
	Err error
}

func (e *JSONError) Error() string { return "invalid JSON: " + e.Err.Error() }

func (e *JSONError) Unwrap() error { return e.Err }

func parseJSON(text string) (any, error) {
	if !json.Valid([]byte(text)) {
		return nil, &JSONError{Err: errors.New("syntax error")}
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, &JSONError{Err: err}
	}
	if jsonDepth(v) > maxJSONDepth {
		return nil, &JSONError{Err: errors.New("maximum depth exceeded")}
	}
	return v, nil
}

func jsonDepth(v any) int {
	deepest := 0
	switch t := v.(type) {
	case map[string]any:
		for _, c := range t {
			deepest = max(deepest, jsonDepth(c))
		}
		return deepest + 1
	case []any:
		for _, c := range t {
			deepest = max(deepest, jsonDepth(c))
		}
		return deepest + 1
	}
	return 0
}

// jsonDouble mirrors JsonElement.TryGetDouble: any number literal that fits a
// finite double.
func jsonDouble(n json.Number) (float64, bool) {
	f, err := strconv.ParseFloat(string(n), 64)
	return f, err == nil
}

// jsonInt32 mirrors JsonElement.TryGetInt32: an integer literal (no fraction
// or exponent) within the 32-bit range.
func jsonInt32(n json.Number) (int, bool) {
	i, err := strconv.ParseInt(string(n), 10, 32)
	return int(i), err == nil
}

// jsonTruthy reports whether obj[key] is truthy in Python's sense, the test
// the original applied with data.get(key).
func jsonTruthy(obj map[string]any, key string) bool {
	v, ok := obj[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	case json.Number:
		d, ok := jsonDouble(t)
		return ok && d != 0
	}
	return true
}

func jsonObject(parent map[string]any, key string) (map[string]any, bool) {
	o, ok := parent[key].(map[string]any)
	return o, ok
}

func jsonStringOrEmpty(parent map[string]any, key string) string {
	s, _ := parent[key].(string)
	return s
}

func jsonStringOrNil(parent map[string]any, key string) *string {
	if s, ok := parent[key].(string); ok {
		return &s
	}
	return nil
}
