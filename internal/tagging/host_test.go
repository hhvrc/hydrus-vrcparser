package tagging_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
	"github.com/hhvrc/hydrus-vrcparser/internal/store"
	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
)

// fakeHydrus is a scriptable hydrus.Client that records what the host asked.
type fakeHydrus struct {
	mu            sync.Mutex
	files         map[int]hydrus.FileMetadata
	order         []int
	addTagsErr    error
	beforeAddTags func(call int) error
	resolveErr    error

	resolveCalls     int
	metadataRequests [][]int
	addTagsCalls     [][]string // tags per call
	addTagsHashes    [][]string
}

func newFakeHydrus(ids ...int) *fakeHydrus {
	f := &fakeHydrus{files: map[int]hydrus.FileMetadata{}}
	for _, id := range ids {
		f.add(id)
	}
	return f
}

func (f *fakeHydrus) add(id int) {
	f.files[id] = hydrus.FileMetadata{FileID: &id, Hash: fmt.Sprintf("%064x", id), Ext: ".png"}
	f.order = append(f.order, id)
}

func (f *fakeHydrus) ResolveLocalTagServiceKey(ctx context.Context, name string) (string, error) {
	f.resolveCalls++
	return "deadbeef", f.resolveErr
}

func (f *fakeHydrus) SearchFileIDs(ctx context.Context, tags []string) ([]int, error) {
	return slices.Clone(f.order), nil
}

func (f *fakeHydrus) FileMetadata(ctx context.Context, ids []int) ([]hydrus.FileMetadata, error) {
	f.metadataRequests = append(f.metadataRequests, slices.Clone(ids))
	var out []hydrus.FileMetadata
	for _, id := range ids {
		if m, ok := f.files[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeHydrus) AddTags(ctx context.Context, hashes []string, serviceKey string, tags []string) error {
	if f.addTagsErr != nil {
		return f.addTagsErr
	}
	if f.beforeAddTags != nil {
		if err := f.beforeAddTags(len(f.addTagsCalls)); err != nil {
			return err
		}
	}
	f.addTagsCalls = append(f.addTagsCalls, slices.Clone(tags))
	f.addTagsHashes = append(f.addTagsHashes, slices.Clone(hashes))
	return nil
}

// fakeTagger derives scripted tags; it optionally also extracts.
type fakeTagger struct {
	id         string
	version    int
	rederive   bool
	tags       map[int][]string
	defaults   []string
	failDerive map[int]bool
	derived    []int
}

func (t *fakeTagger) ID() string              { return t.id }
func (t *fakeTagger) DeriveVersion() int      { return t.version }
func (t *fakeTagger) SelectorQuery() []string { return []string{"system:everything"} }
func (t *fakeTagger) RederiveEveryRun() bool  { return t.rederive }

func (t *fakeTagger) Derive(ctx context.Context, c *tagging.Context) (tagging.TagSet, error) {
	t.derived = append(t.derived, c.File.FileID)
	if t.failDerive[c.File.FileID] {
		return tagging.TagSet{}, errors.New("scripted failure")
	}
	if tags, ok := t.tags[c.File.FileID]; ok {
		return tagging.NewTagSet(tags), nil
	}
	return tagging.NewTagSet(t.defaults), nil
}

type fakeExtractor struct {
	fakeTagger
	extractVersion int
	failExtract    map[int]bool
	onExtract      func(fileID int) // runs after the file is "read"
	mu             sync.Mutex
	extracted      []int
}

func (t *fakeExtractor) ExtractVersion() int { return t.extractVersion }

func (t *fakeExtractor) Extract(ctx context.Context, f tagging.FileRef) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.extracted = append(t.extracted, f.FileID)
	if t.onExtract != nil {
		t.onExtract(f.FileID)
	}
	if t.failExtract[f.FileID] {
		return errors.New("scripted read failure")
	}
	return nil
}

func tagger(id string, tags ...string) *fakeTagger {
	return &fakeTagger{id: id, version: 1, tags: map[int][]string{}, defaults: tags, failDerive: map[int]bool{}}
}

func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "host.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newHost builds a host with a silent logger, failing the test on error.
func newHost(t *testing.T, taggers []tagging.FileTagger, h hydrus.Client, s tagging.Store) *tagging.Host {
	t.Helper()
	host, err := tagging.NewHost(taggers, h, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func run(t *testing.T, taggers []tagging.FileTagger, h hydrus.Client, s tagging.Store, opts tagging.RunOptions) tagging.Report {
	t.Helper()
	host, err := tagging.NewHost(taggers, h, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	report, err := host.Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return report
}

func result(r tagging.Report, id string) tagging.Result {
	for _, x := range r.Results {
		if x.TaggerID == id {
			return x
		}
	}
	panic("no result for " + id)
}

func TestNewFilesAreDerivedAndPushed(t *testing.T) {
	h := newFakeHydrus(1, 2)
	r := result(run(t, []tagging.FileTagger{tagger("t", "vrchat")}, h, openStore(t), tagging.RunOptions{}), "t")

	if r.Status != tagging.Completed || r.Discovered != 2 || r.Derived != 2 || r.Pushed != 2 || len(r.Warnings) != 0 {
		t.Fatalf("result = %+v", r)
	}
	// Identical sets collapse into one request.
	if len(h.addTagsCalls) != 1 || len(h.addTagsHashes[0]) != 2 {
		t.Fatalf("add_tags calls = %v", h.addTagsHashes)
	}
}

func TestASecondRunPushesNothingAndFetchesNoMetadata(t *testing.T) {
	// The push ledger is what makes a scheduled run cost one search instead
	// of thousands of writes.
	h, s, tg := newFakeHydrus(1, 2), openStore(t), tagger("t", "vrchat")
	run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{})
	h.addTagsCalls, h.metadataRequests = nil, nil

	r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{}), "t")

	if r.Derived != 0 || r.NeedingUpdate != 0 || len(h.addTagsCalls) != 0 || len(h.metadataRequests) != 0 {
		t.Fatalf("second run did work: %+v, add_tags=%v, metadata=%v", r, h.addTagsCalls, h.metadataRequests)
	}
}

func TestATaggerOverLiveMetadataRederivesButPushesOnlyChanges(t *testing.T) {
	h, s, tg := newFakeHydrus(1, 2), openStore(t), tagger("t", "a")
	tg.rederive = true
	run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{})
	h.addTagsCalls = nil
	tg.tags[2] = []string{"b"}

	r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{}), "t")

	if r.Derived != 2 || r.NeedingUpdate != 1 || len(h.addTagsCalls) != 1 || h.addTagsCalls[0][0] != "b" {
		t.Fatalf("result = %+v, calls = %v", r, h.addTagsCalls)
	}
}

func TestBumpingTheDeriveVersionRederivesButPushesNothingUnchanged(t *testing.T) {
	h, s, tg := newFakeHydrus(1), openStore(t), tagger("t", "vrchat")
	run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{})
	h.addTagsCalls = nil
	tg.version = 2

	r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{}), "t")

	if r.Derived != 1 || r.NeedingUpdate != 0 || len(h.addTagsCalls) != 0 {
		t.Fatalf("result = %+v", r)
	}
}

func TestPushBatchSizeCapsHashesPerRequest(t *testing.T) {
	h := newFakeHydrus(1, 2, 3, 4, 5)
	run(t, []tagging.FileTagger{tagger("t", "x")}, h, openStore(t), tagging.RunOptions{PushBatchSize: 2})

	var sizes []int
	for _, c := range h.addTagsHashes {
		sizes = append(sizes, len(c))
	}
	if !slices.Equal(sizes, []int{2, 2, 1}) {
		t.Fatalf("batch sizes = %v", sizes)
	}
}

func TestEmptyTagSetsAreNeverPushedAndNeedNoTagService(t *testing.T) {
	h := newFakeHydrus(1)
	h.resolveErr = errors.New("ambiguous service")
	r := result(run(t, []tagging.FileTagger{tagger("t")}, h, openStore(t), tagging.RunOptions{}), "t")

	if r.Status != tagging.Completed || len(h.addTagsCalls) != 0 || h.resolveCalls != 0 {
		t.Fatalf("result = %+v, resolves = %d", r, h.resolveCalls)
	}
}

func TestDryRunNeitherPushesNorRecordsNorResolvesTheService(t *testing.T) {
	h, s, tg := newFakeHydrus(1), openStore(t), tagger("t", "vrchat")
	r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{DryRun: true}), "t")

	if r.NeedingUpdate != 1 || r.Pushed != 0 || len(h.addTagsCalls) != 0 || h.resolveCalls != 0 {
		t.Fatalf("result = %+v", r)
	}
	states, err := s.FileStates(context.Background(), "t", []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 0 {
		t.Fatalf("dry run recorded state: %v", states)
	}
}

func TestAFailedPushIsRetriedNextRun(t *testing.T) {
	h, s, tg := newFakeHydrus(1), openStore(t), tagger("t", "vrchat")
	h.addTagsErr = errors.New("hydrus is down")

	r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{}), "t")
	if r.Status != tagging.Completed || r.PushFailed != 1 {
		t.Fatalf("result = %+v", r)
	}

	h.addTagsErr = nil
	if r := result(run(t, []tagging.FileTagger{tg}, h, s, tagging.RunOptions{}), "t"); r.Pushed != 1 {
		t.Fatalf("retry did not push: %+v", r)
	}
}

func TestOneFileFailingToDeriveDoesNotStopTheRest(t *testing.T) {
	tg := tagger("t", "x")
	tg.failDerive[2] = true
	r := result(run(t, []tagging.FileTagger{tg}, newFakeHydrus(1, 2, 3), openStore(t), tagging.RunOptions{}), "t")

	if r.Derived != 2 || r.DeriveFailed != 1 || r.Pushed != 2 || len(r.Warnings) != 1 {
		t.Fatalf("result = %+v", r)
	}
}

func TestATaggerThatFailsDoesNotStopTheOthers(t *testing.T) {
	h := newFakeHydrus(1)
	h.resolveErr = errors.New("no such service")
	report := run(t, []tagging.FileTagger{tagger("a", "x"), tagger("b", "y")}, h, openStore(t), tagging.RunOptions{})

	if !report.AnyFailed() || result(report, "a").Status != tagging.Failed || result(report, "b").Status != tagging.Failed {
		t.Fatalf("report = %+v", report)
	}
	// Both were attempted, and the failure is reported, not thrown.
	if h.resolveCalls != 2 {
		t.Fatalf("resolve calls = %d", h.resolveCalls)
	}
}

func TestExtractRunsOnceAndAFailedExtractIsRetried(t *testing.T) {
	h, s := newFakeHydrus(1, 2), openStore(t)
	ex := &fakeExtractor{fakeTagger: *tagger("v", "x"), extractVersion: 1, failExtract: map[int]bool{2: true}}

	r := result(run(t, []tagging.FileTagger{ex}, h, s, tagging.RunOptions{}), "v")
	if r.Extracted != 1 || r.ExtractFailed != 1 {
		t.Fatalf("first run = %+v", r)
	}

	ex.extracted, ex.failExtract = nil, map[int]bool{}
	r = result(run(t, []tagging.FileTagger{ex}, h, s, tagging.RunOptions{}), "v")
	if !slices.Equal(ex.extracted, []int{2}) || r.Extracted != 1 {
		t.Fatalf("second run extracted %v: %+v", ex.extracted, r)
	}
}

func TestExtractVersionBumpReExtractsAndRederives(t *testing.T) {
	h, s := newFakeHydrus(1), openStore(t)
	ex := &fakeExtractor{fakeTagger: *tagger("v", "x"), extractVersion: 1, failExtract: map[int]bool{}}
	run(t, []tagging.FileTagger{ex}, h, s, tagging.RunOptions{})
	ex.extracted, ex.derived = nil, nil
	ex.extractVersion = 2

	run(t, []tagging.FileTagger{ex}, h, s, tagging.RunOptions{})
	if len(ex.extracted) != 1 || len(ex.derived) != 1 {
		t.Fatalf("extracted %v, derived %v", ex.extracted, ex.derived)
	}
}

// failingStore fails the first n Record calls, then behaves.
type failingStore struct {
	tagging.Store
	failures int
}

func (s *failingStore) Record(ctx context.Context, id string, o []tagging.Outcome) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("disk full")
	}
	return s.Store.Record(ctx, id, o)
}

func TestCancellingRightAfterAnAcceptedPushStillRecordsIt(t *testing.T) {
	// Hydrus has the tags; if the ledger does not say so, the next run
	// pushes them again.
	h, s := newFakeHydrus(1, 2), openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.beforeAddTags = func(call int) error {
		cancel() // lands after Hydrus accepts this batch
		return nil
	}

	host := newHost(t, []tagging.FileTagger{tagger("t", "a")}, h, s)
	if _, err := host.Run(ctx, tagging.RunOptions{PushBatchSize: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}

	pushed, err := s.PushedHashes(context.Background(), "t", []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed) != len(h.addTagsCalls) || len(pushed) == 0 {
		t.Fatalf("ledger has %d files, Hydrus accepted %d batches", len(pushed), len(h.addTagsCalls))
	}
}

func TestCancellingMidExtractionKeepsEveryFileAlreadyRead(t *testing.T) {
	// Ctrl+C nearly always lands inside an extract chunk. Files read before
	// it -- and the one whose read finished as it landed -- must not be read
	// off the share again.
	h, s := newFakeHydrus(1, 2, 3, 4, 5), openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	ex := &fakeExtractor{fakeTagger: *tagger("v", "x"), extractVersion: 1, failExtract: map[int]bool{}}
	ex.onExtract = func(id int) {
		if id == 3 {
			cancel()
		}
	}

	host := newHost(t, []tagging.FileTagger{ex}, h, s)
	if _, err := host.Run(ctx, tagging.RunOptions{RecordBatchSize: 2, ExtractConcurrency: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}

	states, err := s.FileStates(context.Background(), "v", []int{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 3} {
		if states[id].ExtractVersion != 1 {
			t.Errorf("file %d read but not recorded: %+v", id, states)
		}
	}

	ex.extracted = nil
	run(t, []tagging.FileTagger{ex}, h, s, tagging.RunOptions{})
	slices.Sort(ex.extracted)
	if !slices.Equal(ex.extracted, []int{4, 5}) {
		t.Fatalf("next run re-read %v, want only [4 5]", ex.extracted)
	}
}

func TestAFailedSaveIsRetriedNotForgotten(t *testing.T) {
	// The in-stage save fails; the outcomes must survive for the final one.
	h, real := newFakeHydrus(1), openStore(t)
	s := &failingStore{Store: real, failures: 1}

	r := result(run(t, []tagging.FileTagger{tagger("t", "a")}, h, s, tagging.RunOptions{}), "t")
	if r.Status != tagging.Failed {
		t.Fatalf("status = %s, want failed (the save error is reported)", r.Status)
	}

	states, err := real.FileStates(context.Background(), "t", []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if states[1].DeriveVersion != 1 {
		t.Fatalf("derive outcome lost after a failed save: %+v", states)
	}
}

func TestACancelledRunKeepsWhatItAlreadyPushed(t *testing.T) {
	// Batches Hydrus accepted must reach the ledger even if the run is
	// stopped right after, or the next run pushes them all again.
	h, s := newFakeHydrus(1, 2, 3), openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.beforeAddTags = func(call int) error {
		if call == 1 {
			cancel()
			return context.Canceled
		}
		return nil
	}

	host := newHost(t, []tagging.FileTagger{tagger("t", "a")}, h, s)
	_, err := host.Run(ctx, tagging.RunOptions{PushBatchSize: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want cancellation", err)
	}

	bg := context.Background()
	pushed, err := s.PushedHashes(bg, "t", []int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed) != 1 {
		t.Fatalf("pushed = %v, want the one accepted batch", pushed)
	}
	states, err := s.FileStates(bg, "t", []int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("derive state not saved: %v", states)
	}
	unpushed, err := s.UnpushedTags(bg, "t", []int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(unpushed) != 2 {
		t.Fatalf("unpushed = %v", unpushed)
	}
}

func TestAnUnknownTaggerIsAnError(t *testing.T) {
	host := newHost(t, []tagging.FileTagger{tagger("t")}, newFakeHydrus(), openStore(t))
	if _, err := host.Run(context.Background(), tagging.RunOptions{Only: []string{"typo"}}); err == nil {
		t.Fatal("want error for unknown tagger")
	}
}

func TestMetadataIsFetchedInBatches(t *testing.T) {
	h := newFakeHydrus(1, 2, 3, 4, 5)
	run(t, []tagging.FileTagger{tagger("t")}, h, openStore(t), tagging.RunOptions{MetadataBatchSize: 2})

	var sizes []int
	for _, r := range h.metadataRequests {
		sizes = append(sizes, len(r))
	}
	if !slices.Equal(sizes, []int{2, 2, 1}) {
		t.Fatalf("metadata batch sizes = %v", sizes)
	}
}
