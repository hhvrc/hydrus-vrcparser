package correlate

import (
	"slices"
	"testing"
)

func f(ids []string, names []string) File { return File{IDs: ids, Names: names} }

func ids(v ...string) []string   { return v }
func names(v ...string) []string { return v }

func with(mut func(*Options)) Options {
	o := DefaultOptions()
	mut(&o)
	return o
}

// TestCorrelate carries every case of the C# (cs:) and Python (py:) test
// suites this package replaced. The Python cases were a subset of the C# ones
// with identical inputs and assertions, so each shared case is one row named
// after both.
func TestCorrelate(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		opts  Options
		check func(t *testing.T, r Result)
	}{
		{
			name:  "cs:TreatsIdenticalFileSetsAsAlreadyPaired/py:test_perfect_overlap_is_already_paired",
			files: []File{f(ids("A"), names("Alice")), f(ids("A"), names("Alice"))},
			opts:  DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if !slices.Contains(r.PairedPairs, Pair{"A", "Alice"}) {
					t.Errorf("PairedPairs = %v, want (A, Alice)", r.PairedPairs)
				}
				if len(r.Suggestions) != 0 {
					t.Errorf("Suggestions = %v, want none", r.Suggestions)
				}
			},
		},
		{
			// The name failed to parse on one of A's four files.
			name: "cs:SuggestsAPairWhoseFileSetsMostlyOverlap/py:test_partial_overlap_is_suggested",
			files: []File{
				f(ids("A"), names("Alice")),
				f(ids("A"), names("Alice")),
				f(ids("A"), names("Alice")),
				f(ids("A"), nil),
			},
			opts: with(func(o *Options) { o.MinOverlap = 2; o.MinJaccard = 0.5 }),
			check: func(t *testing.T, r Result) {
				if len(r.PairedPairs) != 0 {
					t.Errorf("PairedPairs = %v, want none", r.PairedPairs)
				}
				want := []Suggestion{{UserID: "A", Name: "Alice", Overlap: 3, IDFiles: 4, NameFiles: 3, Jaccard: 0.75}}
				if !slices.Equal(r.Suggestions, want) {
					t.Errorf("Suggestions = %+v, want %+v", r.Suggestions, want)
				}
			},
		},
		{
			name:  "cs:RejectsPairsBelowTheOverlapThreshold/py:test_below_overlap_threshold_rejected",
			files: []File{f(ids("A"), names("Alice")), f(ids("A"), nil)},
			opts:  with(func(o *Options) { o.MinOverlap = 2 }),
			check: func(t *testing.T, r Result) {
				if len(r.Suggestions) != 0 {
					t.Errorf("Suggestions = %v, want none", r.Suggestions)
				}
				if len(r.Rejected) != 1 {
					t.Errorf("Rejected = %v, want one", r.Rejected)
				}
			},
		},
		{
			// A co-occurs equally with Alice and Beth, so there is no clear winner.
			name: "cs:RejectsWhenTheRunnerUpIsJustAsGood/py:test_ambiguous_runner_up_rejected",
			files: []File{
				f(ids("A"), names("Alice", "Beth")),
				f(ids("A"), names("Alice", "Beth")),
				f(ids("A"), names("Alice", "Beth")),
			},
			opts: Options{MinOverlap: 2, MinJaccard: 0.3, MaxRunnerUpRatio: 0.6},
			check: func(t *testing.T, r Result) {
				if len(r.Suggestions) != 0 {
					t.Errorf("Suggestions = %v, want none", r.Suggestions)
				}
			},
		},
		{
			// B shares far more files with Alice than A does, so A must not take her.
			name: "cs:RequiresAMutualBestMatch/py:test_mutual_best_match_required",
			files: []File{
				f(ids("A"), names("Alice")),
				f(ids("B"), names("Alice")),
				f(ids("B"), names("Alice")),
				f(ids("B"), names("Alice")),
				f(ids("B"), nil),
			},
			opts: Options{MinOverlap: 1, MinJaccard: 0.1, MaxRunnerUpRatio: 1.0},
			check: func(t *testing.T, r Result) {
				for _, s := range r.Suggestions {
					if s.Name == "Alice" && s.UserID != "B" {
						t.Errorf("Alice suggested for %s, want only B", s.UserID)
					}
				}
			},
		},
		{
			name:  "cs:DetectsOrphansOnBothSides/py:test_orphans_detected",
			files: []File{f(ids("A"), nil), f(nil, names("Lonely"))},
			opts:  DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if !slices.Contains(r.OrphanIDs, TagCount{"A", 1}) {
					t.Errorf("OrphanIDs = %v, want (A, 1)", r.OrphanIDs)
				}
				if !slices.Contains(r.OrphanNames, TagCount{"Lonely", 1}) {
					t.Errorf("OrphanNames = %v, want (Lonely, 1)", r.OrphanNames)
				}
				if len(r.Suggestions) != 0 {
					t.Errorf("Suggestions = %v, want none", r.Suggestions)
				}
			},
		},
		{
			// A and B appear on exactly the same files, so co-occurrence cannot
			// say which is Alice and which is Bob.
			name: "cs:RefusesToGuessBetweenInseparableIds/py:test_inseparable_ids_not_guessed",
			files: []File{
				f(ids("A", "B"), names("Alice", "Bob")),
				f(ids("A", "B"), names("Alice", "Bob")),
				f(ids("A", "B"), names("Alice")),
			},
			opts: with(func(o *Options) { o.MinOverlap = 2; o.MinJaccard = 0.5 }),
			check: func(t *testing.T, r Result) {
				if len(r.PairedPairs) != 0 {
					t.Errorf("PairedPairs = %v, want none", r.PairedPairs)
				}
				if len(r.Suggestions) != 0 {
					t.Errorf("Suggestions = %v, want none", r.Suggestions)
				}
				if !slices.Equal(r.AmbiguousPairedIDs, []string{"A", "B"}) {
					t.Errorf("AmbiguousPairedIDs = %v, want [A B]", r.AmbiguousPairedIDs)
				}
			},
		},
		{
			// B is seen once alone with Bob, which breaks the symmetry: A<->Alice
			// becomes an exclusive perfect overlap and B<->Bob is then inferred.
			name: "cs:SeparatesUsersOnceOneAppearsAlone/py:test_multi_user_elimination",
			files: []File{
				f(ids("A", "B"), names("Alice", "Bob")),
				f(ids("A", "B"), names("Alice", "Bob")),
				f(ids("A", "B"), names("Alice")),
				f(ids("B"), names("Bob")),
			},
			opts: with(func(o *Options) { o.MinOverlap = 2; o.MinJaccard = 0.5 }),
			check: func(t *testing.T, r Result) {
				if !slices.Contains(r.PairedPairs, Pair{"A", "Alice"}) {
					t.Errorf("PairedPairs = %v, want (A, Alice)", r.PairedPairs)
				}
				if !slices.ContainsFunc(r.Suggestions, func(s Suggestion) bool { return s.UserID == "B" && s.Name == "Bob" }) {
					t.Errorf("Suggestions = %+v, want B -> Bob", r.Suggestions)
				}
			},
		},
		{
			name:  "cs:ReportsCorpusCounts",
			files: []File{f(ids("A"), names("Alice")), f(ids("B"), names("Beth")), f(nil, nil)},
			opts:  DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if r.FileCount != 3 || r.IDCount != 2 || r.NameCount != 2 {
					t.Errorf("counts = %d/%d/%d, want 3/2/2", r.FileCount, r.IDCount, r.NameCount)
				}
			},
		},
		{
			// A/Alice overlap perfectly bar one file; C/Carol are far weaker.
			name: "cs:RanksSuggestionsByConfidence",
			files: []File{
				f(ids("A"), names("Alice")),
				f(ids("A"), names("Alice")),
				f(ids("A"), names("Alice")),
				f(ids("A"), nil),
				f(ids("C"), names("Carol")),
				f(ids("C"), names("Carol")),
				f(ids("C"), nil),
				f(ids("C"), nil),
				f(ids("C"), nil),
			},
			opts: Options{MinOverlap: 2, MinJaccard: 0.1, MaxRunnerUpRatio: 1.0},
			check: func(t *testing.T, r Result) {
				if len(r.Suggestions) != 2 {
					t.Fatalf("Suggestions = %+v, want two", r.Suggestions)
				}
				if r.Suggestions[0].Jaccard < r.Suggestions[1].Jaccard {
					t.Errorf("suggestions must be ordered most-confident first: %+v", r.Suggestions)
				}
				if r.Suggestions[0].UserID != "A" {
					t.Errorf("first suggestion = %s, want A", r.Suggestions[0].UserID)
				}
			},
		},
		{
			name:  "cs:HandlesAnEmptyCorpus",
			files: nil,
			opts:  DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if r.FileCount != 0 || len(r.Suggestions) != 0 || len(r.PairedPairs) != 0 {
					t.Errorf("got %+v, want an empty result", r)
				}
			},
		},

		// Go-only cases pinning behaviour the ports must share.
		{
			// One id perfectly overlapping two names is ambiguous too, and
			// locks both names out of inference.
			name: "go:OneIdPerfectlyOverlappingTwoNamesIsAmbiguous",
			files: []File{
				f(ids("A"), names("Alice", "Ally")),
				f(ids("A"), names("Alice", "Ally")),
				f(ids("B"), names("Bob")),
				f(ids("B"), names("Bob")),
				f(ids("B"), names("Bob", "Ally2")),
				f(ids("B"), nil),
			},
			opts: DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if !slices.Equal(r.AmbiguousPairedIDs, []string{"A"}) || len(r.PairedPairs) != 0 {
					t.Errorf("Ambiguous = %v, Paired = %v, want [A] and none", r.AmbiguousPairedIDs, r.PairedPairs)
				}
				// B: Bob 3 of 4 (0.75), Ally2 1 of 4 (0.25 <= 0.6 * 0.75).
				want := []Suggestion{{UserID: "B", Name: "Bob", Overlap: 3, IDFiles: 4, NameFiles: 3, Jaccard: 0.75, RunnerUpJaccard: 0.25}}
				if !slices.Equal(r.Suggestions, want) {
					t.Errorf("Suggestions = %+v, want %+v", r.Suggestions, want)
				}
			},
		},
		{
			// Equal candidates keep first-seen order (stable sort), so the
			// winner is the name seen first, and it is then rejected for an
			// ambiguous runner-up.
			name: "go:TiesBreakByFirstSeen",
			files: []File{
				f(ids("A"), names("Zed")),
				f(ids("A"), names("Amy")),
				f(ids("A"), names("Zed", "Amy")),
			},
			opts: DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if len(r.Rejected) != 1 || r.Rejected[0].Name != "Zed" {
					t.Errorf("Rejected = %+v, want A -> Zed", r.Rejected)
				}
			},
		},
		{
			// Suggestions sort by rounded Jaccard, then overlap; equal keys keep
			// discovery order.
			name: "go:SortsByRoundedJaccardThenOverlapStably",
			files: []File{
				// X/Xa: 2 of 3 -> 0.6667, overlap 2
				f(ids("X"), names("Xa")), f(ids("X"), names("Xa")), f(ids("X"), nil),
				// Y/Ya: 4 of 6 -> 0.6667, overlap 4
				f(ids("Y"), names("Ya")), f(ids("Y"), names("Ya")), f(ids("Y"), names("Ya")),
				f(ids("Y"), names("Ya")), f(ids("Y"), nil), f(ids("Y"), nil),
				// W/Wa: 2 of 3 -> 0.6667, overlap 2, seen after X
				f(ids("W"), names("Wa")), f(ids("W"), names("Wa")), f(nil, names("Wa")),
			},
			opts: DefaultOptions(),
			check: func(t *testing.T, r Result) {
				var got []string
				for _, s := range r.Suggestions {
					got = append(got, s.UserID)
				}
				if !slices.Equal(got, []string{"Y", "X", "W"}) {
					t.Errorf("order = %v, want [Y X W]", got)
				}
				if r.Suggestions[0].Jaccard != 0.6667 {
					t.Errorf("Jaccard = %v, want 0.6667", r.Suggestions[0].Jaccard)
				}
			},
		},
		{
			// Duplicate tags within one file count once, as the originals' sets did.
			name:  "go:DuplicatesWithinAFileCountOnce",
			files: []File{f(ids("A", "A"), names("Alice", "Alice")), f(ids("A"), names("Alice"))},
			opts:  DefaultOptions(),
			check: func(t *testing.T, r Result) {
				if !slices.Equal(r.PairedPairs, []Pair{{"A", "Alice"}}) {
					t.Errorf("PairedPairs = %v, want [(A, Alice)]", r.PairedPairs)
				}
			},
		},
		{
			// Thresholds use unrounded values: 2/3 clears MinJaccard 0.66667
			// only if compared before rounding to 0.6667.
			name:  "go:ThresholdsUseUnroundedJaccard",
			files: []File{f(ids("A"), names("Alice")), f(ids("A"), names("Alice")), f(ids("A"), nil)},
			opts:  with(func(o *Options) { o.MinJaccard = 0.66666 }),
			check: func(t *testing.T, r Result) {
				if len(r.Suggestions) != 1 {
					t.Errorf("Suggestions = %+v, want one", r.Suggestions)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, Correlate(tc.files, tc.opts))
		})
	}
}

func TestRound4(t *testing.T) {
	// Expected values are Python's round(p / q, 4), which .NET's Math.Round
	// matches. The 1/160 family is where scale-and-round goes wrong.
	cases := []struct {
		p, q int
		want float64
	}{
		{0, 1, 0},
		{1, 1, 1},
		{2, 3, 0.6667},
		{1, 3, 0.3333},
		{1, 32, 0.0312}, // exact tie, to even
		{3, 32, 0.0938}, // exact tie, to even
		{1, 160, 0.0063},
		{3, 160, 0.0187},
		{19, 160, 0.1187},
		{23, 160, 0.1437},
		{49, 160, 0.3063},
	}
	for _, tc := range cases {
		if got := round4(jaccard(tc.p, tc.q, tc.p)); got != tc.want {
			t.Errorf("round4(%d/%d) = %v, want %v", tc.p, tc.q, got, tc.want)
		}
	}
}

func TestFromTags(t *testing.T) {
	got := FromTags([]string{
		"vrchat-user-name:bob", "vrchat-user-id:usr_2", "creator:someone",
		"vrchat-user-id:usr_1", "vrchat-user-id:usr_2", "vrchat-user-name:alice", "vrchat-user-name:bob",
		"vrchat-user-id:",
	})
	if want := []string{"", "usr_1", "usr_2"}; !slices.Equal(got.IDs, want) {
		t.Errorf("IDs = %q, want %q", got.IDs, want)
	}
	if want := []string{"alice", "bob"}; !slices.Equal(got.Names, want) {
		t.Errorf("Names = %q, want %q", got.Names, want)
	}
	if !FromTags([]string{"world:x"}).Empty() {
		t.Error("a file with no user tags should be empty")
	}
}
