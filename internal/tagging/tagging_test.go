package tagging

import (
	"os"
	"slices"
	"testing"
)

const pathSep = os.PathSeparator

// The tag hash is a stored format: the pushes table holds these values, and a
// change re-pushes every file. The expected hashes were computed with Python's
// hashlib and sorted(), as the original tool did.
func TestTagSetHashIsTheStoredFormat(t *testing.T) {
	cases := []struct {
		name string
		tags []string
		want string
	}{
		{"empty", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{
			// Duplicates kept; non-ASCII sorts after ASCII by code point.
			"duplicates and non-ASCII",
			[]string{"vrchat-world-name:Home", "vrchat", "vrchat", "vrchat-user-name:\u65e5\u672c", "vrchat-user-name:Zed"},
			"6499293d5b355870fce38469dfb82d472f79d53bda11cfb7cafc95b3b289a204",
		},
	}
	for _, c := range cases {
		if got := NewTagSet(c.tags).Hash(); got != c.want {
			t.Errorf("%s: hash = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestTheZeroTagSetHashesLikeAnEmptyOne(t *testing.T) {
	if (TagSet{}).Hash() != NewTagSet(nil).Hash() || !(TagSet{}).Empty() {
		t.Fatal("zero TagSet differs from an empty one")
	}
}

func TestTagSetKeepsProducerOrderAndCopiesItsInput(t *testing.T) {
	in := []string{"b", "a", "b"}
	ts := NewTagSet(in)
	in[0] = "changed"

	if !slices.Equal(ts.Tags(), []string{"b", "a", "b"}) || !slices.Equal(ts.Sorted(), []string{"a", "b", "b"}) {
		t.Fatalf("tags %v sorted %v", ts.Tags(), ts.Sorted())
	}
}

func TestFileRefPathFollowsHydrusLayout(t *testing.T) {
	f := FileRef{FileID: 1, Hash: "ab12", Ext: "png"}
	rel, err := f.RelativePath()
	if err != nil || rel != "fab"+string(pathSep)+"ab12.png" {
		t.Fatalf("rel = %q, %v", rel, err)
	}
	if _, err := (FileRef{FileID: 2, Hash: "a"}).RelativePath(); err == nil {
		t.Fatal("a one-character hash cannot form a path")
	}
}
