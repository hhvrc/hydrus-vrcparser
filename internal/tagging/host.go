package tagging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
)

// maxWarningsPerTagger caps the warning list so one systemic failure cannot
// bury the report.
const maxWarningsPerTagger = 20

// RunOptions controls one run of the host.
type RunOptions struct {
	// DryRun derives and reports but neither pushes nor records state.
	DryRun bool

	// Only restricts the run to these tagger ids. Empty means all.
	Only []string

	// TagServiceName is the local tag service to push to; empty requires
	// exactly one to exist. Resolved only once there is something to push.
	TagServiceName string

	// TagServiceKey, when set, is used as is instead of resolving
	// TagServiceName -- for callers that already resolved it.
	TagServiceKey string

	MetadataBatchSize  int // files per file_metadata request
	PushBatchSize      int // hashes per add_tags request
	ExtractConcurrency int // files read off disk at once

	// RecordBatchSize is how many extracted files go between saves of
	// progress, bounding what an interrupted run has to redo.
	RecordBatchSize int
}

func (o RunOptions) withDefaults() RunOptions {
	if o.MetadataBatchSize <= 0 {
		o.MetadataBatchSize = hydrus.DefaultMetadataBatchSize
	}
	if o.PushBatchSize <= 0 {
		o.PushBatchSize = 100
	}
	if o.ExtractConcurrency <= 0 {
		o.ExtractConcurrency = 8
	}
	if o.RecordBatchSize <= 0 {
		o.RecordBatchSize = 500
	}
	return o
}

// Status is how a tagger's run ended.
type Status string

const (
	Completed Status = "completed"
	Failed    Status = "failed"  // the tagger itself failed; others still ran
	Skipped   Status = "skipped" // excluded by RunOptions.Only
)

// Result is what one tagger did during a run.
type Result struct {
	TaggerID      string
	Status        Status
	Discovered    int
	Extracted     int
	ExtractFailed int
	Derived       int
	DeriveFailed  int
	NeedingUpdate int // files whose tag hash differed from the ledger
	Pushed        int
	PushFailed    int
	Err           error
	Warnings      []string
}

// Report is the outcome of a whole run.
type Report struct{ Results []Result }

// AnyFailed reports whether any tagger failed outright.
func (r Report) AnyFailed() bool {
	return slices.ContainsFunc(r.Results, func(x Result) bool { return x.Status == Failed })
}

// Host runs taggers, owning everything that is not tagger-specific.
type Host struct {
	taggers []FileTagger
	hydrus  hydrus.Client
	store   Store
	log     *slog.Logger
}

// NewHost builds a host over taggers, run in the order given. Tagger ids must
// be unique.
func NewHost(taggers []FileTagger, client hydrus.Client, store Store, log *slog.Logger) (*Host, error) {
	seen := map[string]bool{}
	for _, t := range taggers {
		if seen[t.ID()] {
			return nil, fmt.Errorf("duplicate tagger id %q", t.ID())
		}
		seen[t.ID()] = true
	}
	if log == nil {
		log = slog.Default()
	}
	return &Host{taggers: taggers, hydrus: client, store: store, log: log}, nil
}

// Run runs the selected taggers. One tagger failing does not stop the others;
// only cancelling ctx does, and then each tagger's progress so far is saved
// before Run returns ctx's error.
func (h *Host) Run(ctx context.Context, opts RunOptions) (Report, error) {
	opts = opts.withDefaults()

	selected := map[string]bool{}
	for _, id := range opts.Only {
		if !slices.ContainsFunc(h.taggers, func(t FileTagger) bool { return t.ID() == id }) {
			return Report{}, fmt.Errorf("unknown tagger %q (registered: %s)", id, h.ids())
		}
		selected[id] = true
	}

	var report Report
	serviceKey := opts.TagServiceKey
	resolveService := func() (string, error) {
		// Resolved once, lazily: a run with nothing to push should not fail
		// merely because the service name is ambiguous.
		if serviceKey == "" {
			key, err := h.hydrus.ResolveLocalTagServiceKey(ctx, opts.TagServiceName)
			if err != nil {
				return "", err
			}
			serviceKey = key
		}
		return serviceKey, nil
	}

	for _, t := range h.taggers {
		if len(selected) > 0 && !selected[t.ID()] {
			report.Results = append(report.Results, Result{TaggerID: t.ID(), Status: Skipped})
			continue
		}

		result := h.runTagger(ctx, t, opts, resolveService)
		report.Results = append(report.Results, result)
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if result.Err != nil {
			h.log.Error("tagger failed", "tagger", t.ID(), "err", result.Err)
		}
	}
	return report, nil
}

func (h *Host) ids() string {
	ids := make([]string, len(h.taggers))
	for i, t := range h.taggers {
		ids[i] = t.ID()
	}
	sort.Strings(ids)
	return fmt.Sprint(ids)
}

// run carries one tagger's run: its pending outcomes and its result.
type run struct {
	h      *Host
	tagger FileTagger
	opts   RunOptions
	result Result

	mu      sync.Mutex
	pending map[int]Outcome
}

func (h *Host) runTagger(ctx context.Context, t FileTagger, opts RunOptions, resolveService func() (string, error)) Result {
	r := &run{h: h, tagger: t, opts: opts, result: Result{TaggerID: t.ID(), Status: Completed}, pending: map[int]Outcome{}}

	err := r.stages(ctx, resolveService)

	// Save whatever is left, even when failing or cancelled. Not under ctx: a
	// cancelled run is exactly the one whose progress must be kept.
	if flushErr := r.flush(context.WithoutCancel(ctx)); flushErr != nil {
		err = errors.Join(err, flushErr)
	}

	if err != nil {
		r.result.Status = Failed
		r.result.Err = err
	}
	return r.result
}

func (r *run) warn(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch n := len(r.result.Warnings); {
	case n < maxWarningsPerTagger:
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf(format, args...))
	case n == maxWarningsPerTagger:
		r.result.Warnings = append(r.result.Warnings, fmt.Sprintf("... further warnings suppressed (more than %d)", maxWarningsPerTagger))
	}
}

// update merges fn's changes into the file's pending outcome.
func (r *run) update(fileID int, fn func(*Outcome)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.pending[fileID]
	if !ok {
		o = Outcome{FileID: fileID}
	}
	fn(&o)
	r.pending[fileID] = o
}

// flush saves pending outcomes, then forgets the ones it saved. Forgetting is
// safe because the store merges: a later outcome with nil fields leaves
// earlier ones intact.
//
// Saving ignores cancellation -- progress made before Ctrl+C is exactly what
// must be kept -- and outcomes stay pending until the store has them, so a
// failed save is retried by the final flush instead of being lost.
func (r *run) flush(ctx context.Context) error {
	r.mu.Lock()
	if r.opts.DryRun {
		clear(r.pending)
		r.mu.Unlock()
		return nil
	}
	pending := make([]Outcome, 0, len(r.pending))
	for _, o := range r.pending {
		pending = append(pending, o)
	}
	r.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].FileID < pending[j].FileID })
	if err := r.h.store.Record(context.WithoutCancel(ctx), r.tagger.ID(), pending); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range pending {
		// Only what was saved: an outcome updated meanwhile stays pending.
		if r.pending[o.FileID] == o {
			delete(r.pending, o.FileID)
		}
	}
	return nil
}

func (r *run) stages(ctx context.Context, resolveService func() (string, error)) error {
	t, h := r.tagger, r.h
	log := h.log.With("tagger", t.ID())

	// 1. Discover.
	query := t.SelectorQuery()
	if len(query) == 0 {
		return fmt.Errorf("empty selector query would match the entire Hydrus database")
	}
	fileIDs, err := h.hydrus.SearchFileIDs(ctx, query)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	r.result.Discovered = len(fileIDs)
	log.Info("discovered candidate files", "count", len(fileIDs))
	if len(fileIDs) == 0 {
		return nil
	}

	// 2. Resolve identity, fetching only what is not already cached.
	metadata := map[int]*hydrus.FileMetadata{}
	files, err := r.resolveFiles(ctx, fileIDs, metadata)
	if err != nil {
		return err
	}
	known := slices.Sorted(maps.Keys(files))

	states, err := h.store.FileStates(ctx, t.ID(), known)
	if err != nil {
		return err
	}

	// 3. Extract, for taggers that read bytes off disk.
	extracted := map[int]bool{}
	if ex, ok := t.(FileExtractor); ok {
		if err := r.extract(ctx, ex, files, known, states, extracted); err != nil {
			return err
		}
	}

	// 4. Derive.
	derived, err := r.derive(ctx, files, known, states, extracted, metadata)
	if err != nil {
		return err
	}

	// 5. Diff and push. Files not re-derived are still candidates if their
	// stored tags never reached Hydrus -- otherwise a failed push would be
	// permanent, the derive gate having already been passed.
	candidates := maps.Clone(derived)
	var notDerived []int
	for _, id := range known {
		if _, ok := derived[id]; !ok {
			notDerived = append(notDerived, id)
		}
	}
	if len(notDerived) > 0 {
		unpushed, err := h.store.UnpushedTags(ctx, t.ID(), notDerived)
		if err != nil {
			return err
		}
		for id, tags := range unpushed {
			candidates[id] = tags
		}
	}

	return r.push(ctx, files, candidates, resolveService)
}

func (r *run) resolveFiles(ctx context.Context, fileIDs []int, metadata map[int]*hydrus.FileMetadata) (map[int]FileRef, error) {
	h := r.h
	files, err := h.store.FileRefs(ctx, fileIDs)
	if err != nil {
		return nil, err
	}

	var unknown []int
	for _, id := range fileIDs {
		if _, ok := files[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return files, nil
	}

	h.log.Info("fetching Hydrus metadata for previously unseen files", "tagger", r.tagger.ID(), "count", len(unknown))
	if err := r.fetchMetadata(ctx, unknown, metadata); err != nil {
		return nil, err
	}

	var discovered []FileRef
	for _, id := range unknown {
		meta, ok := metadata[id]
		if !ok || meta.Hash == "" {
			r.warn("file %d: Hydrus returned no hash; skipped", id)
			continue
		}
		f := FileRef{FileID: id, Hash: meta.Hash, Ext: meta.NormalizedExt()}
		files[id] = f
		discovered = append(discovered, f)
	}
	// Identity is a cache of Hydrus facts, not run state; recording it on a
	// dry run is harmless, and the CLI runs dry runs on a snapshot anyway.
	if err := h.store.UpsertFileRefs(ctx, discovered); err != nil {
		return nil, err
	}
	return files, nil
}

// fetchMetadata fills metadata for any of ids not already in it.
func (r *run) fetchMetadata(ctx context.Context, ids []int, metadata map[int]*hydrus.FileMetadata) error {
	var wanted []int
	for _, id := range ids {
		if _, ok := metadata[id]; !ok {
			wanted = append(wanted, id)
		}
	}
	for batch := range slices.Chunk(wanted, r.opts.MetadataBatchSize) {
		rows, err := r.h.hydrus.FileMetadata(ctx, batch)
		if err != nil {
			return fmt.Errorf("file metadata: %w", err)
		}
		for i := range rows {
			if rows[i].FileID != nil {
				metadata[*rows[i].FileID] = &rows[i]
			}
		}
	}
	return nil
}

func (r *run) extract(ctx context.Context, ex FileExtractor, files map[int]FileRef, known []int, states map[int]FileState, extracted map[int]bool) error {
	version := ex.ExtractVersion()
	var stale []FileRef
	for _, id := range known {
		if states[id].ExtractVersion < version {
			stale = append(stale, files[id])
		}
	}
	r.h.log.Info("extracting files", "tagger", ex.ID(), "count", len(stale), "version", version)

	for chunk := range slices.Chunk(stale, r.opts.RecordBatchSize) {
		var wg sync.WaitGroup
		sem := make(chan struct{}, r.opts.ExtractConcurrency)
		var mu sync.Mutex
		for _, f := range chunk {
			// Wait for a slot before checking for cancellation, so no new
			// read starts after Ctrl+C.
			sem <- struct{}{}
			if ctx.Err() != nil {
				<-sem
				break
			}
			wg.Add(1)
			go func(f FileRef) {
				defer wg.Done()
				defer func() { <-sem }()

				err := ex.Extract(ctx, f)
				if err != nil && ctx.Err() != nil {
					return // cut short by cancellation: neither success nor a failure to report
				}
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					r.result.ExtractFailed++
					r.warn("file %d: %v", f.FileID, err)
					return
				}
				extracted[f.FileID] = true
				r.result.Extracted++
				r.update(f.FileID, func(o *Outcome) { o.ExtractVersion = &version })
			}(f)
		}
		wg.Wait()

		if err := r.flush(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) derive(ctx context.Context, files map[int]FileRef, known []int, states map[int]FileState, extracted map[int]bool, metadata map[int]*hydrus.FileMetadata) (map[int]TagSet, error) {
	t := r.tagger
	version := t.DeriveVersion()

	var todo []int
	for _, id := range known {
		if t.RederiveEveryRun() || extracted[id] || states[id].DeriveVersion < version {
			todo = append(todo, id)
		}
	}
	r.h.log.Info("deriving tags", "tagger", t.ID(), "count", len(todo))

	if err := r.fetchMetadata(ctx, todo, metadata); err != nil {
		return nil, err
	}

	derived := map[int]TagSet{}
	for _, id := range todo {
		meta, ok := metadata[id]
		if !ok {
			r.warn("file %d: no Hydrus metadata; skipped", id)
			continue
		}
		tags, err := t.Derive(ctx, &Context{File: files[id], Metadata: meta})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// One unparseable file should not stop the rest.
			r.result.DeriveFailed++
			r.warn("file %d: %v", id, err)
			continue
		}
		derived[id] = tags
		r.update(id, func(o *Outcome) { o.DeriveVersion = &version; o.Tags = &tags })
	}
	r.result.Derived = len(derived)

	// Saved before pushing. If the push then dies, the unpushed-tags check
	// picks these up next run.
	return derived, r.flush(ctx)
}

func (r *run) push(ctx context.Context, files map[int]FileRef, candidates map[int]TagSet, resolveService func() (string, error)) error {
	t, h := r.tagger, r.h
	if len(candidates) == 0 {
		return nil
	}

	ids := slices.Sorted(maps.Keys(candidates))
	pushed, err := h.store.PushedHashes(ctx, t.ID(), ids)
	if err != nil {
		return err
	}

	// Group by tag set, not by file: Hydrus applies one tag list to many
	// hashes per call, so identical sets collapse into one request.
	type group struct {
		tags  TagSet
		files []FileRef
	}
	groups := map[string]*group{}
	var order []string
	for _, id := range ids {
		tags := candidates[id]
		if tags.Empty() || pushed[id] == tags.Hash() {
			continue
		}
		f, ok := files[id]
		if !ok {
			r.warn("file %d: no known hash; cannot push", id)
			continue
		}
		r.result.NeedingUpdate++
		g, ok := groups[tags.Hash()]
		if !ok {
			g = &group{tags: tags}
			groups[tags.Hash()] = g
			order = append(order, tags.Hash())
		}
		g.files = append(g.files, f)
	}

	h.log.Info("files need tag updates", "tagger", t.ID(), "files", r.result.NeedingUpdate, "distinct_sets", len(groups))
	if r.opts.DryRun || len(groups) == 0 {
		return nil
	}

	serviceKey, err := resolveService()
	if err != nil {
		return fmt.Errorf("resolve tag service: %w", err)
	}

	for _, hash := range order {
		g := groups[hash]
		for batch := range slices.Chunk(g.files, r.opts.PushBatchSize) {
			hashes := make([]string, len(batch))
			for i, f := range batch {
				hashes[i] = f.Hash
			}

			if err := h.hydrus.AddTags(ctx, hashes, serviceKey, g.tags.Tags()); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				// Nothing recorded, so the next run retries this batch.
				r.result.PushFailed += len(batch)
				r.warn("push of %d files failed: %v", len(batch), err)
				continue
			}

			for _, f := range batch {
				r.update(f.FileID, func(o *Outcome) { o.PushedHash = hash })
			}
			r.result.Pushed += len(batch)

			// Hydrus has these tags now; the ledger must say so before
			// anything else can go wrong.
			if err := r.flush(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
