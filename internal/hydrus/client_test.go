package hydrus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func queryValue(t *testing.T, r recorded, key string) string {
	t.Helper()
	q, err := url.ParseQuery(r.Query)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Has(key) {
		t.Fatalf("query parameter %q not present in %s", key, r.Query)
	}
	return q.Get(key)
}

func TestSearchSendsJSONEncodedTagsAndReturnsFileIDs(t *testing.T) {
	s := newStub(t).json(`{ "file_ids": [1, 2, 3] }`)
	c, _ := newTestClient(t, s, Options{})

	ids, err := c.SearchFileIDs(context.Background(), []string{"system:filetype is png", "system:has embedded metadata"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int{1, 2, 3}) {
		t.Fatalf("ids = %v", ids)
	}

	reqs := s.reqs()
	if len(reqs) != 1 || reqs[0].Path != "/get_files/search_files" || reqs[0].Method != http.MethodGet {
		t.Fatalf("requests: %+v", reqs)
	}
	// The tags parameter must be a JSON array, URL-encoded.
	var tags []string
	if err := json.Unmarshal([]byte(queryValue(t, reqs[0], "tags")), &tags); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tags, []string{"system:filetype is png", "system:has embedded metadata"}) {
		t.Fatalf("tags = %q", tags)
	}
	if got := queryValue(t, reqs[0], "return_file_ids"); got != "true" {
		t.Fatalf("return_file_ids = %q", got)
	}
	// Escaped like Uri.EscapeDataString: spaces as %20, not '+'.
	if strings.Contains(reqs[0].Query, "+") || !strings.Contains(reqs[0].Query, "%20") {
		t.Fatalf("query not data-escaped: %s", reqs[0].Query)
	}
	if got := reqs[0].Header.Get(AccessKeyHeader); got != "test-key" {
		t.Fatalf("access key header = %q", got)
	}
}

func TestSearchTermsWithUppercaseORBecomeORPredicates(t *testing.T) {
	cases := map[string]string{
		"a OR b": `[["a","b"]]`,
		"system:has url with domain twitter.com OR system:has url with domain x.com": `[["system:has url with domain twitter.com","system:has url with domain x.com"]]`,
		"lowercase or is part of the tag":                                            `["lowercase or is part of the tag"]`,
		"  a  OR  b OR c ":                                                           `[["a","b","c"]]`,
	}
	for term, want := range cases {
		got, err := serializeSearchTags([]string{term})
		if err != nil || got != want {
			t.Errorf("serializeSearchTags(%q) = %s, %v; want %s", term, got, err, want)
		}
	}
}

func TestAnORTermWithoutTwoAlternativesIsAnError(t *testing.T) {
	// Passing "a OR " through would search for a literal tag that can never
	// exist and so silently match nothing.
	for _, term := range []string{"a OR ", " OR b", " OR ", "a OR  OR "} {
		if got, err := serializeSearchTags([]string{"ok", term}); err == nil {
			t.Errorf("serializeSearchTags(%q) = %s, want error", term, got)
		}
	}

	s := newStub(t)
	c, _ := newTestClient(t, s, Options{})
	if _, err := c.SearchFileIDs(context.Background(), []string{"a OR "}); err == nil {
		t.Fatal("expected an error")
	}
	if n := len(s.reqs()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestSearchReturnsEmptyWhenHydrusOmitsFileIDs(t *testing.T) {
	s := newStub(t).json(`{}`)
	c, _ := newTestClient(t, s, Options{})

	ids, err := c.SearchFileIDs(context.Background(), []string{"a"})
	if err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
}

func TestFileMetadataDeserializesTheFieldsThePipelineUses(t *testing.T) {
	s := newStub(t).json(`{
	  "metadata": [{
	    "file_id": 42, "hash": "aabbcc", "size": 1234, "ext": ".png",
	    "width": 1920, "height": 1080, "has_transparency": true,
	    "known_urls": ["https://x.com/someone/status/1"]
	  }]
	}`)
	c, _ := newTestClient(t, s, Options{})

	rows, err := c.FileMetadata(context.Background(), []int{42, 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	row := rows[0]
	if *row.FileID != 42 || row.Hash != "aabbcc" || row.NormalizedExt() != "png" {
		t.Fatalf("row = %+v", row)
	}
	if !slices.Equal(row.AllURLs(), []string{"https://x.com/someone/status/1"}) {
		t.Fatalf("urls = %q", row.AllURLs())
	}

	r := s.reqs()[0]
	if r.Path != "/get_files/file_metadata" || queryValue(t, r, "file_ids") != "[42,7]" {
		t.Fatalf("request: %+v", r)
	}
}

func TestFileMetadataShortCircuitsOnAnEmptyIDList(t *testing.T) {
	s := newStub(t)
	c, _ := newTestClient(t, s, Options{})

	rows, err := c.FileMetadata(context.Background(), nil)
	if err != nil || len(rows) != 0 || len(s.reqs()) != 0 {
		t.Fatalf("rows = %v, err = %v, requests = %d", rows, err, len(s.reqs()))
	}
}

func TestAddTagsPostsHashesAndServiceKeysToTags(t *testing.T) {
	s := newStub(t).json(`{}`)
	c, _ := newTestClient(t, s, Options{})

	if err := c.AddTags(context.Background(), []string{"hash1", "hash2"}, "svc-key",
		[]string{"vrchat", "vrchat-world-name:Home"}); err != nil {
		t.Fatal(err)
	}

	reqs := s.reqs()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/add_tags/add_tags" {
		t.Fatalf("requests: %+v", reqs)
	}
	if ct := reqs[0].Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	var body struct {
		Hashes            []string            `json:"hashes"`
		ServiceKeysToTags map[string][]string `json:"service_keys_to_tags"`
	}
	if err := json.Unmarshal([]byte(reqs[0].Body), &body); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(body.Hashes, []string{"hash1", "hash2"}) ||
		!slices.Equal(body.ServiceKeysToTags["svc-key"], []string{"vrchat", "vrchat-world-name:Home"}) ||
		len(body.ServiceKeysToTags) != 1 {
		t.Fatalf("body = %s", reqs[0].Body)
	}
}

func TestAddTagsSkipsTheRequestWhenThereIsNothingToSend(t *testing.T) {
	s := newStub(t)
	c, _ := newTestClient(t, s, Options{})

	if err := c.AddTags(context.Background(), nil, "svc-key", []string{"vrchat"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTags(context.Background(), []string{"hash1"}, "svc-key", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(s.reqs()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

func TestUnauthorizedSurfacesAsAPIErrorWithAKeyHint(t *testing.T) {
	s := newStub(t).status(http.StatusUnauthorized, "bad key")
	c, d := newTestClient(t, s, Options{MaxRetries: DefaultMaxRetries})

	_, err := c.SearchFileIDs(context.Background(), []string{"a"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Body != "bad key" || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("err = %v", err)
	}
	// A bad key will never succeed on retry.
	if len(s.reqs()) != 1 || len(d.all()) != 0 {
		t.Fatalf("retried: %d requests, %d delays", len(s.reqs()), len(d.all()))
	}
}

func TestUnreachableHydrusSurfacesAsConnectionError(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	addr := dead.URL
	dead.Close()

	c, err := NewHTTPClient(Options{Address: addr, APIKey: "k", MaxRetries: 2, Logger: discard()})
	if err != nil {
		t.Fatal(err)
	}
	d := &delays{}
	c.sleep = d.sleep

	_, err = c.SearchFileIDs(context.Background(), []string{"a"})

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || connErr.Timeout {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), addr) {
		t.Fatalf("message lacks the address: %v", err)
	}
	if got := d.all(); len(got) != 2 {
		t.Fatalf("delays = %v, want 2 retries", got)
	}
}

func TestMalformedJSONIsAnError(t *testing.T) {
	s := newStub(t).json(`{"file_ids": "nope"}`)
	c, _ := newTestClient(t, s, Options{})

	if _, err := c.SearchFileIDs(context.Background(), []string{"a"}); err == nil {
		t.Fatal("expected an error")
	}
}

// ---- options ----

func TestAMissingAPIKeyIsAConfigError(t *testing.T) {
	for _, key := range []string{"", "   "} {
		_, err := NewHTTPClient(Options{APIKey: key})
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) || !strings.Contains(err.Error(), "HYDRUS_API_KEY") {
			t.Errorf("key %q: err = %v", key, err)
		}
	}
}

func TestAnInvalidAddressIsAConfigError(t *testing.T) {
	for _, addr := range []string{"ftp://host", "not a url", "http://", "127.0.0.1:45869"} {
		_, err := NewHTTPClient(Options{APIKey: "k", Address: addr})
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Errorf("address %q: err = %v", addr, err)
		}
	}
}

func TestOptionDefaults(t *testing.T) {
	c, err := NewHTTPClient(Options{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	// A zero MaxRetries means no retries, not the default.
	if c.base != "http://127.0.0.1:45869/" || c.timeout != 2*time.Minute || c.maxRetries != 0 {
		t.Fatalf("defaults: base=%q timeout=%v retries=%d", c.base, c.timeout, c.maxRetries)
	}

	c, err = NewHTTPClient(Options{APIKey: "k", Address: "http://hydrus:1234//", MaxRetries: DefaultMaxRetries})
	if err != nil {
		t.Fatal(err)
	}
	if c.base != "http://hydrus:1234/" || c.maxRetries != DefaultMaxRetries {
		t.Fatalf("base=%q retries=%d", c.base, c.maxRetries)
	}
}

func TestZeroMaxRetriesMakesExactlyOneAttempt(t *testing.T) {
	s := newStub(t).status(http.StatusServiceUnavailable, "")
	c, d := newTestClient(t, s, Options{MaxRetries: 0})

	_, err := c.SearchFileIDs(context.Background(), []string{"a"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(s.reqs()) != 1 || len(d.all()) != 0 {
		t.Fatalf("err = %v, %d requests, %d delays", err, len(s.reqs()), len(d.all()))
	}
}

func TestNegativeMaxRetriesIsAConfigError(t *testing.T) {
	_, err := NewHTTPClient(Options{APIKey: "k", MaxRetries: -1})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestATrailingSlashOnTheAddressStillResolvesPaths(t *testing.T) {
	s := newStub(t).json(`{}`)
	c, _ := newTestClient(t, s, Options{}) // address already has a trailing slash

	if _, err := c.SearchFileIDs(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if p := s.reqs()[0].Path; p != "/get_files/search_files" {
		t.Fatalf("path = %q", p)
	}
}

// ---- file metadata shapes ----

func TestMatchesRealHydrusMetadataResponse(t *testing.T) {
	// The real /get_files/file_metadata shape from Hydrus 681 (API v94), with
	// user data replaced by placeholders.
	data, err := os.ReadFile(filepath.Join("testdata", "file_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := newStub(t).json(string(data))
	c, _ := newTestClient(t, s, Options{})

	rows, err := c.FileMetadata(context.Background(), []int{42})
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if *row.FileID != 42 || row.NormalizedExt() != "png" || len(row.AllURLs()) != 1 ||
		!slices.Equal(row.CurrentTags(), []string{"vrchat", "vrchat-world-name:Example World"}) {
		t.Fatalf("row = %+v", row)
	}
}

func TestCollectsURLsFromBothKnownURLsAndURLs(t *testing.T) {
	var m FileMetadata
	if err := json.Unmarshal([]byte(`{"known_urls": ["https://x.com/a/status/1"], "urls": ["https://x.com/b/status/2"]}`), &m); err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://x.com/a/status/1", "https://x.com/b/status/2"}; !slices.Equal(m.AllURLs(), want) {
		t.Fatalf("got %q", m.AllURLs())
	}
}

func TestNormalizesExtensionAndDefaultsToPNG(t *testing.T) {
	for ext, want := range map[string]string{".PNG": "png", "png": "png", ".jpeg": "jpeg", "": "png"} {
		m := FileMetadata{Ext: ext}
		if got := m.NormalizedExt(); got != want {
			t.Errorf("NormalizedExt(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestRedirectsAreNotFollowedSoTheKeyStaysWithHydrus(t *testing.T) {
	// The default client follows redirects and forwards custom headers, which
	// would hand the access key to whatever host a redirect names.
	var leaked, reached bool
	var mu sync.Mutex
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reached = true
		leaked = leaked || r.Header.Get(AccessKeyHeader) != ""
		mu.Unlock()
		_, _ = io.WriteString(w, `{"file_ids": []}`)
	}))
	t.Cleanup(other.Close)

	s := newStub(t)
	s.mu.Lock()
	s.responses = append(s.responses, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/get_files/search_files", http.StatusFound)
	})
	s.mu.Unlock()
	c, _ := newTestClient(t, s, Options{MaxRetries: DefaultMaxRetries})

	_, err := c.SearchFileIDs(context.Background(), []string{"a"})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
		t.Fatalf("err = %v (%T)", err, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if reached || leaked {
		t.Fatalf("redirect followed: reached = %v, key sent = %v", reached, leaked)
	}
	if len(s.reqs()) != 1 {
		t.Fatalf("%d requests", len(s.reqs()))
	}
}
