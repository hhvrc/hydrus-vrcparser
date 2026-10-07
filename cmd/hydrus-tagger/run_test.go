package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeHydrus serves just enough of the client API for whole runs: one local
// tag service, Twitter-linked files, VRChat PNG candidates, and files carrying
// user tags for correlate.
type fakeHydrus struct {
	mu       sync.Mutex
	files    map[int][]string // file id -> known URLs (Twitter candidates)
	pngs     map[int]bool     // VRChat candidates
	userTags map[int][]string // current tags, for correlate
	addTags  []map[string]any
	requests int
}

func fakeHash(id int) string { return fmt.Sprintf("%x", sha256.Sum256([]byte{byte(id)})) }

func (f *fakeHydrus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++

	if r.Header.Get("Hydrus-Client-API-Access-Key") != "test-key" {
		http.Error(w, "bad key", http.StatusForbidden)
		return
	}

	switch r.URL.Path {
	case "/get_services":
		fmt.Fprint(w, `{"services": {"6c6f63616c2074616773": {"name": "my tags", "type": 5}}}`)

	case "/get_files/search_files":
		query := r.URL.Query().Get("tags")
		var ids []int
		switch {
		case strings.Contains(query, "twitter.com"):
			for id := range f.files {
				ids = append(ids, id)
			}
		case strings.Contains(query, "filetype is png"):
			for id := range f.pngs {
				ids = append(ids, id)
			}
		case strings.Contains(query, "vrchat-user-id"):
			for id := range f.userTags {
				ids = append(ids, id)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"file_ids": ids})

	case "/get_files/file_metadata":
		var ids []int
		json.Unmarshal([]byte(r.URL.Query().Get("file_ids")), &ids)
		var rows []map[string]any
		for _, id := range ids {
			ext := ".jpg"
			if f.pngs[id] {
				ext = ".png"
			}
			rows = append(rows, map[string]any{
				"file_id":    id,
				"hash":       fakeHash(id),
				"ext":        ext,
				"known_urls": f.files[id],
				"tags":       map[string]any{"svc": map[string]any{"storage_tags": map[string]any{"0": f.userTags[id]}}},
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"metadata": rows})

	case "/add_tags/add_tags":
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.addTags = append(f.addTags, body)
		fmt.Fprint(w, `{}`)

	default:
		http.NotFound(w, r)
	}
}

func (f *fakeHydrus) pushes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.addTags)
}

func setupRun(t *testing.T) (*fakeHydrus, config) {
	t.Helper()
	fake := &fakeHydrus{files: map[int][]string{
		1: {"https://twitter.com/Someone/status/1"},
		2: {"https://x.com/someone/status/2"},
		3: {"https://example.com/not-twitter"},
	}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	cfg := defaultConfig()
	cfg.HydrusAddress = srv.URL
	cfg.APIKey = "test-key"
	cfg.ServiceName = "my tags"
	cfg.Database = filepath.Join(t.TempDir(), "vrchat.db")
	cfg.DataDirectory = t.TempDir()
	return fake, cfg
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestARunTagsTwitterFilesAndASecondRunPushesNothing(t *testing.T) {
	fake, cfg := setupRun(t)

	if err := run(context.Background(), quietLog(), cfg, nil, false, 2); err != nil {
		t.Fatal(err)
	}
	// Files 1 and 2 share a tag set, so they go out in one request.
	if fake.pushes() != 1 {
		t.Fatalf("add_tags calls = %d, want 1", fake.pushes())
	}
	got := fake.addTags[0]["service_keys_to_tags"].(map[string]any)["6c6f63616c2074616773"].([]any)
	if len(got) != 1 || got[0] != "twitter-username:someone" {
		t.Fatalf("tags = %v", got)
	}

	if err := run(context.Background(), quietLog(), cfg, nil, false, 2); err != nil {
		t.Fatal(err)
	}
	if fake.pushes() != 1 {
		t.Fatalf("second run pushed again: %d calls", fake.pushes())
	}
}

func TestADryRunPushesNothingAndLeavesTheDatabaseAlone(t *testing.T) {
	fake, cfg := setupRun(t)
	if err := run(context.Background(), quietLog(), cfg, nil, false, 2); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.files[4] = []string{"https://twitter.com/newcomer/status/4"}
	fake.mu.Unlock()

	before, _ := os.ReadFile(cfg.Database)
	if err := run(context.Background(), quietLog(), cfg, nil, true, 2); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(cfg.Database)

	if fake.pushes() != 1 {
		t.Fatalf("dry run pushed: %d calls", fake.pushes())
	}
	if string(before) != string(after) {
		t.Fatal("dry run changed the database file")
	}
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("hydrus-tagger-dryrun-%d.db*", os.Getpid()))); len(matches) != 0 {
		t.Fatalf("snapshot left behind: %v", matches)
	}
}

func TestAnUnknownServiceStopsBeforeTheDatabaseIsCreated(t *testing.T) {
	_, cfg := setupRun(t)
	cfg.ServiceName = "no such service"

	err := run(context.Background(), quietLog(), cfg, nil, false, 2)
	if exitCode(err, quietLog()) != 2 {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if _, statErr := os.Stat(cfg.Database); !os.IsNotExist(statErr) {
		t.Fatal("database created despite failed preflight")
	}
}

func TestAnUnknownTaggerNameIsAUsageError(t *testing.T) {
	_, cfg := setupRun(t)
	err := run(context.Background(), quietLog(), cfg, []string{"typo"}, false, 2)
	if exitCode(err, quietLog()) != 2 {
		t.Fatalf("err = %v", err)
	}
}
