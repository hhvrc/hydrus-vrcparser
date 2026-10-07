package correlate

import (
	"strings"
	"testing"
)

func TestPyFloat(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.0"},
		{1, "1.0"},
		{0.5, "0.5"},
		{0.6667, "0.6667"},
		{0.0001, "0.0001"},
		{0.0063, "0.0063"},
		{0.00001, "1e-05"},
	}
	for _, tc := range cases {
		if got := pyFloat(tc.in); got != tc.want {
			t.Errorf("pyFloat(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	index := NewHashIndex()
	index.Add("cc", File{IDs: []string{"usr_a"}, Names: []string{"Al, \"the\" one"}})
	index.Add("aa", File{IDs: []string{"usr_a"}, Names: []string{"Al, \"the\" one"}})
	index.Add("dd", File{IDs: []string{"usr_a"}, Names: []string{"Al, \"the\" one"}})
	index.Add("bb", File{IDs: []string{"usr_a"}, Names: []string{"Al, \"the\" one"}})
	index.Add("ee", File{IDs: []string{"usr_a"}})

	var b strings.Builder
	err := WriteCSV(&b, []Suggestion{
		{UserID: "usr_a", Name: "Al, \"the\" one", Overlap: 4, IDFiles: 5, NameFiles: 4, Jaccard: 0.8, RunnerUpJaccard: 0},
		{UserID: "usr_b", Name: " spaced", Overlap: 2, IDFiles: 2, NameFiles: 3, Jaccard: 0.6667, RunnerUpJaccard: 0.3333},
	}, index)
	if err != nil {
		t.Fatal(err)
	}
	want := "user_id,name,overlap,id_files,name_files,jaccard,runner_up_jaccard,example_hashes\r\n" +
		"usr_a,\"Al, \"\"the\"\" one\",4,5,4,0.8,0.0,aa bb cc\r\n" +
		"usr_b, spaced,2,2,3,0.6667,0.3333,\r\n"
	if got := b.String(); got != want {
		t.Errorf("CSV:\n%q\nwant:\n%q", got, want)
	}
}

func TestWriteReport(t *testing.T) {
	r := Result{
		Suggestions: []Suggestion{
			{UserID: "usr_a", Name: "Alice", Overlap: 3, IDFiles: 4, NameFiles: 3, Jaccard: 0.75},
			{UserID: "usr_b", Name: "Bob", Overlap: 12, IDFiles: 20, NameFiles: 13, Jaccard: 0.5714},
		},
		PairedPairs: []Pair{{"usr_c", "Carol"}},
		Rejected:    []Suggestion{{}},
		OrphanIDs:   []TagCount{{"usr_d", 1}},
		FileCount:   40,
		IDCount:     4,
		NameCount:   3,
	}
	var b strings.Builder
	WriteReport(&b, r, 1, "out.csv")

	eq := strings.Repeat("=", 78)
	dash := strings.Repeat("-", 78)
	want := "\n" + eq + "\n" +
		"VRCHAT USER ID <-> NAME CORRELATION\n" +
		eq + "\n" +
		"Files scanned ................ 40\n" +
		"Distinct user-ids ............ 4\n" +
		"Distinct user-names .......... 3\n" +
		"Already paired (excluded) .... 1\n" +
		"Ambiguous already-paired ..... 0\n" +
		"Orphan ids (no name seen) .... 1\n" +
		"Orphan names (no id seen) .... 0\n" +
		"Strict suggestions ........... 2\n" +
		"Near-misses (rejected) ....... 1\n" +
		"\n" + dash + "\n" +
		"SUGGESTED PAIRS (top 1 by confidence)\n" +
		dash + "\n" +
		"jaccard  ovlp   idf  namf  user-id / name\n" +
		"  0.750     3     4     3  usr_a  ->  Alice\n" +
		"\n" + dash + "\n" +
		"Full results written to: out.csv\n" +
		dash + "\n\n"
	if got := b.String(); got != want {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
}
