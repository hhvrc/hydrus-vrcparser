package twitter

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

func TestUsernameIsTheFirstPathSegmentLowercased(t *testing.T) {
	cases := map[string]string{
		"https://twitter.com/MsMspc/status/1210836297041899520":                        "msmspc",
		"https://x.com/seraziel/status/1":                                              "seraziel",
		"https://mobile.twitter.com/SunsetSkyline1/status/1459197746674499586/photo/2": "sunsetskyline1",
		"https://www.twitter.com/_maiqo/status/1":                                      "_maiqo",
		"http://twitter.com/abmayo_mfp/status/1":                                       "abmayo_mfp",
		"https://fxtwitter.com/ne_go_m/status/1":                                       "ne_go_m",
		"https://fixupx.com/ne_go_m/status/1":                                          "ne_go_m",
		"https://twitter.com/Pudge_Ruffian":                                            "pudge_ruffian",
		"https://x.com/someone/status/1?s=20":                                          "someone",
		"HTTPS://X.COM/Someone/status/1":                                               "someone",
		"https://x.com:443/someone/status/1":                                           "someone",
	}
	for in, want := range cases {
		got, ok := Username(in)
		if !ok || got != want {
			t.Errorf("Username(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestURLsNamingNoAccountYieldNothing(t *testing.T) {
	for _, in := range []string{
		"https://twitter.com/i/web/status/1294072134726107136",
		"https://x.com/home",
		"https://x.com/intent/tweet?text=hi",
		"https://x.com/hashtag/vrchat",
		"https://x.com/",
		"https://pic.twitter.com/abcdef",
		"https://pbs.twimg.com/media/abc.jpg",
		"https://notx.com/someone/status/1",
		"https://evil.x.com.example/someone",
		"https://x.com/this_handle_is_too_long/status/1",
		"ftp://x.com/someone",
		"not a url",
		"",
	} {
		if got, ok := Username(in); ok {
			t.Errorf("Username(%q) = %q, want none", in, got)
		}
	}
}

func context1(urls ...string) *tagging.Context {
	id := 1
	return &tagging.Context{
		File:     tagging.FileRef{FileID: 1, Hash: strings.Repeat("0", 64), Ext: "png"},
		Metadata: &hydrus.FileMetadata{FileID: &id, KnownURLs: urls},
	}
}

func TestSearchesBothNamesOfTheSiteAndRederivesEveryRun(t *testing.T) {
	// The legacy script searched x.com alone and missed every file whose URL
	// predates the rename.
	tg := New(Options{})

	if tg.ID() != "twitter" || tg.DeriveVersion() != 1 || !tg.RederiveEveryRun() {
		t.Fatalf("identity: %q v%d rederive=%v", tg.ID(), tg.DeriveVersion(), tg.RederiveEveryRun())
	}
	q := tg.SelectorQuery()
	if len(q) != 1 || !strings.Contains(q[0], "twitter.com") || !strings.Contains(q[0], "x.com") {
		t.Fatalf("selector: %q", q)
	}
}

func TestOneTagPerDistinctAccountInFirstSeenOrder(t *testing.T) {
	tags, err := New(Options{}).Derive(context.Background(), context1(
		"https://twitter.com/Seraziel/status/1",
		"https://x.com/seraziel/status/1",
		"https://x.com/i/web/status/1",
		"https://x.com/quoted_artist/status/2",
		"https://example.com/seraziel",
	))
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"twitter-username:seraziel", "twitter-username:quoted_artist"}; !slices.Equal(tags.Tags(), want) {
		t.Errorf("Tags() = %q, want %q", tags.Tags(), want)
	}
	if want := []string{"twitter-username:quoted_artist", "twitter-username:seraziel"}; !slices.Equal(tags.Sorted(), want) {
		t.Errorf("Sorted() = %q, want %q", tags.Sorted(), want)
	}
}

func TestReadsURLsFromBothMetadataFields(t *testing.T) {
	c := context1("https://x.com/a/status/1")
	c.Metadata.URLs = []string{"https://x.com/b/status/2", "https://x.com/A/status/3"}

	tags, err := New(Options{}).Derive(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"twitter-username:a", "twitter-username:b"}; !slices.Equal(tags.Tags(), want) {
		t.Errorf("got %q, want %q", tags.Tags(), want)
	}
}

func TestAFileWithNoUsableURLGetsNoTags(t *testing.T) {
	tags, err := New(Options{}).Derive(context.Background(), context1("https://twitter.com/i/web/status/1"))
	if err != nil {
		t.Fatal(err)
	}
	if !tags.Empty() {
		t.Errorf("got %q", tags.Tags())
	}
}

func TestTheNamespaceIsConfigurable(t *testing.T) {
	tags, err := New(Options{Namespace: "creator"}).Derive(context.Background(), context1("https://x.com/someone/status/1"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"creator:someone"}; !slices.Equal(tags.Tags(), want) {
		t.Errorf("got %q", tags.Tags())
	}
}

func TestDeriveWithoutMetadataIsAnError(t *testing.T) {
	if _, err := New(Options{}).Derive(context.Background(), &tagging.Context{}); err == nil {
		t.Fatal("expected an error")
	}
}

// ---- selector configuration (SelectorConfigurationTests) ----

func TestAnUnconfiguredSelectorFallsBackToTheDefault(t *testing.T) {
	if got := New(Options{}).SelectorQuery(); !slices.Equal(got, DefaultSelectorQuery()) {
		t.Fatalf("got %q", got)
	}
	if got := New(Options{SelectorQuery: []string{}}).SelectorQuery(); !slices.Equal(got, DefaultSelectorQuery()) {
		t.Fatalf("empty slice: got %q", got)
	}
}

func TestAConfiguredSelectorReplacesTheDefaultRatherThanAddingToIt(t *testing.T) {
	want := []string{"system:has url with domain x.com"}
	if got := New(Options{SelectorQuery: want}).SelectorQuery(); !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestTheDefaultSearchesEveryHostTheParserAccepts(t *testing.T) {
	q := DefaultSelectorQuery()
	if len(q) != 1 {
		t.Fatalf("got %d terms", len(q))
	}
	if want := 6; len(domains) != want {
		t.Fatalf("%d domains, want %d", len(domains), want)
	}
	for _, host := range []string{"twitter.com", "x.com", "fxtwitter.com", "vxtwitter.com", "fixupx.com", "fixvx.com"} {
		if !strings.Contains(q[0], "system:has url with domain "+host) {
			t.Errorf("default selector misses %s", host)
		}
		if got, ok := Username("https://" + host + "/someone/status/1"); !ok || got != "someone" {
			t.Errorf("Username rejects %s", host)
		}
	}
	// Must split into exactly the six alternatives the Hydrus client expects.
	if parts := strings.Split(q[0], " OR "); len(parts) != 6 {
		t.Errorf("got %d alternatives", len(parts))
	}
}

func TestSelectorQueryReturnsACopy(t *testing.T) {
	tg := New(Options{})
	tg.SelectorQuery()[0] = "mutated"
	if tg.SelectorQuery()[0] == "mutated" {
		t.Fatal("caller mutated the tagger's selector")
	}
}
