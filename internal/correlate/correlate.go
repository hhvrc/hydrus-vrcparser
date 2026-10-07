// Package correlate infers which vrchat-user-id tags belong with which
// vrchat-user-name tags. Ported from the original Python tool; its behaviour,
// including ranking and rounding, was verified against it on real data.
//
// VRChat metadata emits a player's id and displayName together, so a true pair
// appears on nearly the same set of files. Pairs whose file sets are identical
// are already trivially linked and need no inference; the interesting cases
// are those that strongly overlap but diverged, usually because the name (or
// id) failed to parse on some of that user's screenshots.
//
// The algorithm is pure: no Hydrus, no I/O. Report and CSV formatting live in
// output.go so the command stays thin.
package correlate

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hhvrc/hydrus-vrcparser/internal/vrchat"
)

// Tag namespaces the correlator reads: the ones the VRChat tagger writes.
const (
	IDPrefix   = vrchat.NSUserID
	NamePrefix = vrchat.NSUserName
)

// File is the user ids and names present on one file, prefixes stripped.
// Duplicates within a file are ignored, as the originals' sets did. Order
// matters only for tie-breaking: tags are ranked by first appearance across
// the corpus.
type File struct {
	IDs   []string
	Names []string
}

// FromTags splits a file's tags into ids and names, stripping the prefixes and
// dropping duplicates and unrelated tags. Each side is sorted so that
// tie-breaks do not depend on the order Hydrus (or Go map iteration over its
// services) happened to list the tags in.
func FromTags(tags []string) File {
	var f File
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, IDPrefix); ok {
			f.IDs = append(f.IDs, v)
		} else if v, ok := strings.CutPrefix(t, NamePrefix); ok {
			f.Names = append(f.Names, v)
		}
	}
	slices.Sort(f.IDs)
	slices.Sort(f.Names)
	f.IDs = slices.Compact(f.IDs)
	f.Names = slices.Compact(f.Names)
	return f
}

// Empty reports whether the file carries neither an id nor a name.
func (f File) Empty() bool { return len(f.IDs) == 0 && len(f.Names) == 0 }

// Options are the acceptance thresholds. Start from DefaultOptions: the zero
// value is a valid but very permissive configuration.
type Options struct {
	// MinOverlap is the minimum number of files an id and name must share to
	// be suggested.
	MinOverlap int

	// MinJaccard is the minimum file-set Jaccard similarity to suggest a pair.
	MinJaccard float64

	// MaxRunnerUpRatio rejects a pair whose runner-up scores above this
	// fraction of the winner.
	MaxRunnerUpRatio float64
}

// DefaultOptions returns the thresholds the originals defaulted to.
func DefaultOptions() Options {
	return Options{MinOverlap: 2, MinJaccard: 0.5, MaxRunnerUpRatio: 0.6}
}

// Suggestion is a proposed (id, name) pairing, with the evidence behind it.
// Jaccard and RunnerUpJaccard are rounded to four decimals for presentation;
// acceptance was decided on the unrounded values.
type Suggestion struct {
	UserID          string
	Name            string
	Overlap         int     // files where both the id and the name appear
	IDFiles         int     // files carrying the id
	NameFiles       int     // files carrying the name
	Jaccard         float64 // overlap / union of the two file sets
	RunnerUpJaccard float64 // best competing name's Jaccard (ambiguity gauge)
}

// Pair is an id and name already linked by identical file sets.
type Pair struct {
	UserID string
	Name   string
}

// TagCount is a tag value and the number of files carrying it.
type TagCount struct {
	Value string
	Files int
}

// Result is everything Correlate found.
type Result struct {
	// Suggestions are the accepted pairs, most confident first.
	Suggestions []Suggestion

	// PairedPairs are exclusive 1:1 identical-file-set pairs, excluded from
	// inference.
	PairedPairs []Pair

	// AmbiguousPairedIDs perfectly overlap a name, but not exclusively (two
	// ids always seen together, or one id perfectly overlapping several
	// names). They and their names are excluded from inference.
	AmbiguousPairedIDs []string

	// Rejected are best candidates that failed a threshold, most confident
	// first.
	Rejected []Suggestion

	// OrphanIDs never co-occur with any name; OrphanNames never with any id.
	OrphanIDs   []TagCount
	OrphanNames []TagCount

	FileCount int
	IDCount   int
	NameCount int
}

// Correlate correlates ids with names from per-file tag sets.
//
// A pair is accepted only when it co-occurs at least opts.MinOverlap times,
// clears opts.MinJaccard, is a mutual best match among the still-unpaired
// tags, and beats its runner-up by a clear margin (runner-up at most
// opts.MaxRunnerUpRatio of the winner). Deliberately conservative: a wrong
// identity pairing is worse than a missed one.
func Correlate(files []File, opts Options) Result {
	// Insertion order is preserved throughout: tie-breaks depend on it, as
	// they did on dict ordering in the Python original.
	idFiles := newCounter()
	nameFiles := newCounter()
	co := map[string]*counter{}     // id -> {name: overlap}
	nameCo := map[string]*counter{} // name -> {id: overlap}

	for _, f := range files {
		ids := dedupe(f.IDs)
		names := dedupe(f.Names)
		for _, i := range ids {
			idFiles.inc(i)
		}
		for _, n := range names {
			nameFiles.inc(n)
		}
		for _, i := range ids {
			row := getOrAdd(co, i)
			for _, n := range names {
				row.inc(n)
				getOrAdd(nameCo, n).inc(i)
			}
		}
	}

	result := Result{
		FileCount: len(files),
		IDCount:   len(idFiles.order),
		NameCount: len(nameFiles.order),
	}

	// 1) Already paired: an id whose file set is identical to a name's. Only
	//    accept when the match is exclusive 1:1 on both sides -- if two ids
	//    always appear together, co-occurrence cannot separate them.
	lockedIDs := map[string]bool{}
	lockedNames := map[string]bool{}
	type perfect struct {
		id    string
		names []string
	}
	var perfectNamesOf []perfect
	perfectIDsOf := map[string][]string{}

	for _, i := range idFiles.order {
		var perfects []string
		row := coOf(co, i)
		for _, n := range row.order {
			c := row.counts[n]
			if c == idFiles.counts[i] && nameFiles.counts[n] == c {
				perfects = append(perfects, n)
			}
		}
		if len(perfects) == 0 {
			continue
		}
		perfectNamesOf = append(perfectNamesOf, perfect{i, perfects})
		for _, n := range perfects {
			perfectIDsOf[n] = append(perfectIDsOf[n], i)
		}
	}

	for _, p := range perfectNamesOf {
		lockedIDs[p.id] = true
		if len(p.names) == 1 && len(perfectIDsOf[p.names[0]]) == 1 {
			result.PairedPairs = append(result.PairedPairs, Pair{p.id, p.names[0]})
			lockedNames[p.names[0]] = true
		} else {
			result.AmbiguousPairedIDs = append(result.AmbiguousPairedIDs, p.id)
			for _, n := range p.names {
				lockedNames[n] = true
			}
		}
	}

	// 2) Orphans: tags that never co-occur with the opposite kind at all.
	for _, i := range idFiles.order {
		if len(coOf(co, i).order) == 0 {
			result.OrphanIDs = append(result.OrphanIDs, TagCount{i, idFiles.counts[i]})
		}
	}
	for _, n := range nameFiles.order {
		if len(coOf(nameCo, n).order) == 0 {
			result.OrphanNames = append(result.OrphanNames, TagCount{n, nameFiles.counts[n]})
		}
	}

	// 3) Over the still-unpaired tags, keep only mutually-best,
	//    high-confidence matches.
	bestIDFor := func(name string) (string, bool) {
		bestID, found := "", false
		bestOverlap, bestJaccard := 0, -1.0
		row := coOf(nameCo, name)
		for _, id := range row.order {
			if lockedIDs[id] {
				continue
			}
			overlap := row.counts[id]
			jac := jaccard(overlap, idFiles.counts[id], nameFiles.counts[name])
			// Lexicographic (jaccard, overlap) comparison, first seen wins ties.
			if jac > bestJaccard || (jac == bestJaccard && overlap > bestOverlap) {
				bestID, found = id, true
				bestOverlap, bestJaccard = overlap, jac
			}
		}
		return bestID, found
	}

	type candidate struct {
		name    string
		overlap int
		jaccard float64
	}
	for _, i := range idFiles.order {
		if lockedIDs[i] {
			continue
		}
		row := coOf(co, i)
		var cands []candidate
		for _, n := range row.order {
			if lockedNames[n] {
				continue
			}
			ov := row.counts[n]
			cands = append(cands, candidate{n, ov, jaccard(ov, idFiles.counts[i], nameFiles.counts[n])})
		}
		if len(cands) == 0 {
			continue
		}
		// Stable, so equal scores keep first-seen order (Python's sort with
		// reverse=True and LINQ's OrderByDescending are both stable).
		sort.SliceStable(cands, func(a, b int) bool {
			if cands[a].jaccard != cands[b].jaccard {
				return cands[a].jaccard > cands[b].jaccard
			}
			return cands[a].overlap > cands[b].overlap
		})

		top := cands[0]
		runnerUp := 0.0
		if len(cands) > 1 {
			runnerUp = cands[1].jaccard
		}

		s := Suggestion{
			UserID:          i,
			Name:            top.name,
			Overlap:         top.overlap,
			IDFiles:         idFiles.counts[i],
			NameFiles:       nameFiles.counts[top.name],
			Jaccard:         round4(top.jaccard),
			RunnerUpJaccard: round4(runnerUp),
		}

		// Thresholds use the unrounded values; the rounding above is for
		// presentation only.
		best, ok := bestIDFor(top.name)
		accepted := top.overlap >= opts.MinOverlap &&
			top.jaccard >= opts.MinJaccard &&
			ok && best == i &&
			runnerUp <= top.jaccard*opts.MaxRunnerUpRatio

		if accepted {
			result.Suggestions = append(result.Suggestions, s)
		} else {
			result.Rejected = append(result.Rejected, s)
		}
	}

	sortByConfidence(result.Suggestions)
	sortByConfidence(result.Rejected)
	return result
}

// sortByConfidence orders by rounded Jaccard, then overlap, both descending.
// Stable, so equal scores keep discovery order.
func sortByConfidence(items []Suggestion) {
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Jaccard != items[b].Jaccard {
			return items[a].Jaccard > items[b].Jaccard
		}
		return items[a].Overlap > items[b].Overlap
	})
}

func jaccard(overlap, aFiles, bFiles int) float64 {
	union := aFiles + bFiles - overlap
	if union == 0 {
		return 0
	}
	return float64(overlap) / float64(union)
}

// round4 rounds to four decimals as Python's round(x, 4) and .NET's
// Math.Round(x, 4) both do: correctly, on the exact binary value, ties to
// even. The tempting math.RoundToEven(x*1e4)/1e4 is not equivalent: the
// multiply itself rounds, so 1/160 (binary value just above 0.00625) would
// come out 0.0062 instead of 0.0063. strconv's fixed-precision formatting is
// exact, and was checked against Python and .NET for every p/q, q <= 3000.
func round4(x float64) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 4, 64), 64)
	if err != nil {
		return x // unreachable for finite x
	}
	return r
}

// dedupe drops repeats, keeping first-seen order. Small inputs, so a linear
// scan beats allocating a map.
func dedupe(values []string) []string {
	out := values[:0:0]
	for _, v := range values {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// counter counts keys and remembers the order they were first seen.
type counter struct {
	counts map[string]int
	order  []string
}

func newCounter() *counter { return &counter{counts: map[string]int{}} }

func (c *counter) inc(key string) {
	if _, ok := c.counts[key]; !ok {
		c.order = append(c.order, key)
	}
	c.counts[key]++
}

var emptyCounter = newCounter()

func getOrAdd(m map[string]*counter, key string) *counter {
	c, ok := m[key]
	if !ok {
		c = newCounter()
		m[key] = c
	}
	return c
}

func coOf(m map[string]*counter, key string) *counter {
	if c, ok := m[key]; ok {
		return c
	}
	return emptyCounter
}
