package twitter

import (
	"context"
	"errors"
	"strings"

	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

// TaggerID scopes this tagger's rows in the database.
const TaggerID = "twitter"

// DefaultNamespace matches the legacy script, so tags it already pushed are the
// same tags rather than near-duplicates.
const DefaultNamespace = "twitter-username"

// DefaultSelectorQuery is used when no selector is configured. Domain search is
// exact, so every host Username accepts is asked for, as one OR term.
func DefaultSelectorQuery() []string {
	alternatives := make([]string, len(domains))
	for i, d := range domains {
		alternatives[i] = "system:has url with domain " + d
	}
	return []string{strings.Join(alternatives, " OR ")}
}

// Options configures the tagger. Zero values take the defaults.
type Options struct {
	// Namespace is the tag namespace; empty means DefaultNamespace.
	Namespace string

	// SelectorQuery selects candidate files; empty means
	// DefaultSelectorQuery. A configured selector replaces the default rather
	// than being ANDed with it.
	SelectorQuery []string
}

// Tagger tags files with the Twitter/X accounts their known URLs point at.
//
// It replaces legacy-python/twitter_username_tagger, which searched only x.com
// and so never saw the ~5,600 files whose URLs predate the rename, and which
// re-pushed every tag on every run.
//
// Input is live Hydrus metadata, not file bytes, so it re-derives on every run:
// a file can gain a URL long after it was first seen.
type Tagger struct {
	namespace string
	selector  []string
}

var _ tagging.FileTagger = (*Tagger)(nil)

// New returns a tagger configured by opts.
func New(opts Options) *Tagger {
	t := &Tagger{namespace: opts.Namespace, selector: append([]string(nil), opts.SelectorQuery...)}
	if t.namespace == "" {
		t.namespace = DefaultNamespace
	}
	if len(t.selector) == 0 {
		t.selector = DefaultSelectorQuery()
	}
	return t
}

// ID implements tagging.Tagger.
func (t *Tagger) ID() string { return TaggerID }

// DeriveVersion implements tagging.Tagger.
func (t *Tagger) DeriveVersion() int { return 1 }

// SelectorQuery implements tagging.Tagger. The returned slice is a copy.
func (t *Tagger) SelectorQuery() []string { return append([]string(nil), t.selector...) }

// RederiveEveryRun implements tagging.Tagger.
func (t *Tagger) RederiveEveryRun() bool { return true }

// Derive implements tagging.FileTagger: one tag per distinct account, in the
// order first seen.
func (t *Tagger) Derive(_ context.Context, c *tagging.Context) (tagging.TagSet, error) {
	if c == nil || c.Metadata == nil {
		return tagging.TagSet{}, errors.New("twitter tagger needs the file's Hydrus metadata")
	}

	// Distinct: a file commonly carries the same status under both twitter.com
	// and x.com, and TagSet keeps duplicates in its hash.
	seen := make(map[string]bool)
	var tags []string
	for _, raw := range c.Metadata.AllURLs() {
		user, ok := Username(raw)
		if !ok || seen[user] {
			continue
		}
		seen[user] = true
		tags = append(tags, t.namespace+":"+user)
	}
	return tagging.NewTagSet(tags), nil
}
