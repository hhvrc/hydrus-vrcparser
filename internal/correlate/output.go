package correlate

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// HashIndex records which files carry each id and name, so a suggestion can
// cite example screenshots.
type HashIndex struct {
	ids   map[string]map[string]struct{}
	names map[string]map[string]struct{}
}

// NewHashIndex returns an empty index.
func NewHashIndex() *HashIndex {
	return &HashIndex{ids: map[string]map[string]struct{}{}, names: map[string]map[string]struct{}{}}
}

// Add records that the file with this hash carries f's ids and names.
func (x *HashIndex) Add(hash string, f File) {
	for _, i := range f.IDs {
		addTo(x.ids, i, hash)
	}
	for _, n := range f.Names {
		addTo(x.names, n, hash)
	}
}

// Examples returns up to limit hashes of files carrying both the suggestion's
// id and name, in sorted order.
func (x *HashIndex) Examples(s Suggestion, limit int) []string {
	names := x.names[s.Name]
	var shared []string
	for h := range x.ids[s.UserID] {
		if _, ok := names[h]; ok {
			shared = append(shared, h)
		}
	}
	slices.Sort(shared)
	if len(shared) > limit {
		shared = shared[:limit]
	}
	return shared
}

func addTo(m map[string]map[string]struct{}, key, hash string) {
	set, ok := m[key]
	if !ok {
		set = map[string]struct{}{}
		m[key] = set
	}
	set[hash] = struct{}{}
}

// ExampleLimit is how many example hashes the CSV cites per suggestion.
const ExampleLimit = 3

// csvHeader is the CSV's column list, as the original Python tool wrote it.
var csvHeader = []string{
	"user_id", "name", "overlap", "id_files", "name_files",
	"jaccard", "runner_up_jaccard", "example_hashes",
}

// WriteCSV writes suggestions in the original Python tool's format: same columns,
// floats as Python's str() prints them, example hashes space-separated, CRLF
// line endings and minimal quoting as Python's csv module does by default.
func WriteCSV(w io.Writer, suggestions []Suggestion, index *HashIndex) error {
	var b strings.Builder
	writeCSVRow(&b, csvHeader)
	for _, s := range suggestions {
		var examples []string
		if index != nil {
			examples = index.Examples(s, ExampleLimit)
		}
		writeCSVRow(&b, []string{
			s.UserID, s.Name,
			strconv.Itoa(s.Overlap), strconv.Itoa(s.IDFiles), strconv.Itoa(s.NameFiles),
			pyFloat(s.Jaccard), pyFloat(s.RunnerUpJaccard),
			strings.Join(examples, " "),
		})
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeCSVRow mirrors Python's csv.writer with the default "excel" dialect:
// a field is quoted only if it contains the delimiter, the quote character,
// CR or LF, and embedded quotes are doubled. (encoding/csv also quotes fields
// with a leading space, which Python does not.)
func writeCSVRow(b *strings.Builder, fields []string) {
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		if strings.ContainsAny(f, ",\"\r\n") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(f, `"`, `""`))
			b.WriteByte('"')
		} else {
			b.WriteString(f)
		}
	}
	b.WriteString("\r\n")
}

// pyFloat formats a float as Python's str() does for the values this package
// produces: shortest round-trip digits, always with a decimal point ("1.0",
// "0.6667"), and exponent notation below 1e-4 or from 1e16.
func pyFloat(f float64) string {
	abs := f
	if abs < 0 {
		abs = -abs
	}
	if abs != 0 && (abs < 1e-4 || abs >= 1e16) {
		s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. 1e-05, Python: 1e-05
		return s
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// WriteReport prints the original Python tool's console report: corpus counts, then
// up to show suggestions, then where the CSV went.
func WriteReport(w io.Writer, r Result, show int, csvPath string) {
	rule := func(c string) { fmt.Fprintln(w, strings.Repeat(c, 78)) }

	fmt.Fprintln(w)
	rule("=")
	fmt.Fprintln(w, "VRCHAT USER ID <-> NAME CORRELATION")
	rule("=")
	fmt.Fprintf(w, "Files scanned ................ %d\n", r.FileCount)
	fmt.Fprintf(w, "Distinct user-ids ............ %d\n", r.IDCount)
	fmt.Fprintf(w, "Distinct user-names .......... %d\n", r.NameCount)
	fmt.Fprintf(w, "Already paired (excluded) .... %d\n", len(r.PairedPairs))
	fmt.Fprintf(w, "Ambiguous already-paired ..... %d\n", len(r.AmbiguousPairedIDs))
	fmt.Fprintf(w, "Orphan ids (no name seen) .... %d\n", len(r.OrphanIDs))
	fmt.Fprintf(w, "Orphan names (no id seen) .... %d\n", len(r.OrphanNames))
	fmt.Fprintf(w, "Strict suggestions ........... %d\n", len(r.Suggestions))
	fmt.Fprintf(w, "Near-misses (rejected) ....... %d\n", len(r.Rejected))

	if len(r.Suggestions) > 0 {
		n := min(max(show, 0), len(r.Suggestions))
		fmt.Fprintln(w)
		rule("-")
		fmt.Fprintf(w, "SUGGESTED PAIRS (top %d by confidence)\n", n)
		rule("-")
		fmt.Fprintf(w, "%7s  %4s  %4s  %4s  user-id / name\n", "jaccard", "ovlp", "idf", "namf")
		for _, s := range r.Suggestions[:n] {
			fmt.Fprintf(w, "%7.3f  %4d  %4d  %4d  %s  ->  %s\n",
				s.Jaccard, s.Overlap, s.IDFiles, s.NameFiles, s.UserID, s.Name)
		}
	}

	fmt.Fprintln(w)
	rule("-")
	fmt.Fprintf(w, "Full results written to: %s\n", csvPath)
	rule("-")
	fmt.Fprintln(w)
}
