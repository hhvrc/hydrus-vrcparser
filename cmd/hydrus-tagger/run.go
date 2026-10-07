package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
	"github.com/hhvrc/hydrus-vrcparser/internal/store"
	"github.com/hhvrc/hydrus-vrcparser/internal/tagging"
	"github.com/hhvrc/hydrus-vrcparser/internal/twitter"
	"github.com/hhvrc/hydrus-vrcparser/internal/vrchat"
)

func runCommand(ctx context.Context, args []string) int {
	log := newLogger()

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file (default "+defaultConfigPath()+")")
	dbPath := fs.String("db", "", "cache database (default from config, else vrchat.db)")
	addr := fs.String("hydrus-addr", "", "Hydrus client API address")
	service := fs.String("service-name", "", "local tag service to push to (default: the only one)")
	dataDir := fs.String("data-dir", "", "Hydrus files directory, for VRChat files with none recorded")
	var only listFlag
	fs.Var(&only, "only", "run only these taggers (repeatable or comma-separated): vrchat, twitter")
	dryRun := fs.Bool("dry-run", false, "derive and report against a throwaway copy of the database; push nothing")
	concurrency := fs.Int("extract-concurrency", 8, "files read off disk at once")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := noArgs(fs); err != nil {
		return exitCode(err, log)
	}

	cfg, path, err := loadSettings(*configPath)
	if err != nil {
		return exitCode(err, log)
	}
	if err := cfg.requireAPIKey(path); err != nil {
		return exitCode(err, log)
	}
	overlay(&cfg.Database, *dbPath)
	overlay(&cfg.HydrusAddress, *addr)
	overlay(&cfg.ServiceName, *service)
	overlay(&cfg.DataDirectory, *dataDir)

	return exitCode(run(ctx, log, cfg, only, *dryRun, *concurrency), log)
}

// taggerIDs are the taggers run builds, for validating --only up front.
var taggerIDs = []string{vrchat.TaggerID, twitter.TaggerID}

func run(ctx context.Context, log *slog.Logger, cfg config, only []string, dryRun bool, concurrency int) error {
	for _, id := range only {
		if !slices.Contains(taggerIDs, id) {
			return usageError{fmt.Errorf("unknown tagger %q (known: %s)", id, strings.Join(taggerIDs, ", "))}
		}
	}

	client, err := cfg.newClient(log)
	if err != nil {
		return err
	}

	// Preflight before touching the database: a run that cannot reach Hydrus,
	// or names no usable tag service, should leave it exactly as it was.
	serviceKey, err := client.ResolveLocalTagServiceKey(ctx, cfg.ServiceName)
	if err != nil {
		return asUsage(err)
	}

	dbPath, err := filepath.Abs(cfg.Database)
	if err != nil {
		return err
	}
	if dryRun {
		// Extraction caches and newly seen files are written as a run goes;
		// on a dry run none of that may reach the real database.
		snapshot := filepath.Join(os.TempDir(), fmt.Sprintf("hydrus-tagger-dryrun-%d.db", os.Getpid()))
		defer removeDB(snapshot)
		if _, err := os.Stat(dbPath); err == nil {
			log.Info("dry run: working on a snapshot", "database", dbPath)
			if err := store.Snapshot(ctx, dbPath, snapshot); err != nil {
				return err
			}
		}
		dbPath = snapshot
	}

	log.Info("using database", "path", dbPath)
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		if errors.Is(err, store.ErrLegacySchema) {
			return usageError{err}
		}
		return err
	}
	defer db.Close()

	taggers := []tagging.FileTagger{
		vrchat.NewTagger(db, vrchat.Options{
			DataDirectory: cfg.DataDirectory,
			SelectorQuery: cfg.VrchatSelector,
			Logger:        log,
		}),
		twitter.New(twitter.Options{Namespace: cfg.TwitterNS, SelectorQuery: cfg.TwitterSelector}),
	}
	host, err := tagging.NewHost(taggers, client, db, log)
	if err != nil {
		return err
	}

	report, runErr := host.Run(ctx, tagging.RunOptions{
		DryRun:             dryRun,
		Only:               only,
		TagServiceName:     cfg.ServiceName,
		TagServiceKey:      serviceKey,
		ExtractConcurrency: concurrency,
	})
	printReport(report, dryRun)

	if runErr != nil {
		return runErr // cancellation; --only was validated above
	}
	if report.AnyFailed() {
		return errors.New("one or more taggers failed")
	}
	return nil
}

// asUsage marks configuration and connectivity errors, which the user fixes
// rather than reports, so they print without noise.
func asUsage(err error) error {
	var cfgErr *hydrus.ConfigError
	var connErr *hydrus.ConnectionError
	var apiErr *hydrus.APIError
	var svcErr *hydrus.ServiceResolutionError
	if errors.As(err, &cfgErr) || errors.As(err, &connErr) || errors.As(err, &apiErr) || errors.As(err, &svcErr) {
		return usageError{err}
	}
	return err
}

func printReport(r tagging.Report, dryRun bool) {
	fmt.Println()
	if dryRun {
		fmt.Println("Run summary (dry run: nothing pushed, database untouched)")
	} else {
		fmt.Println("Run summary")
	}
	fmt.Printf("%-10s %-10s %7s %8s %7s %7s %7s %8s %7s %7s\n",
		"tagger", "status", "found", "extract", "failed", "derive", "failed", "changed", "pushed", "failed")
	for _, x := range r.Results {
		fmt.Printf("%-10s %-10s %7d %8d %7d %7d %7d %8d %7d %7d\n",
			x.TaggerID, x.Status, x.Discovered, x.Extracted, x.ExtractFailed,
			x.Derived, x.DeriveFailed, x.NeedingUpdate, x.Pushed, x.PushFailed)
	}
	for _, x := range r.Results {
		if x.Err != nil {
			fmt.Printf("%s: %v\n", x.TaggerID, x.Err)
		}
		for _, w := range x.Warnings {
			fmt.Printf("%s: warning: %s\n", x.TaggerID, w)
		}
	}
}

// removeDB deletes a throwaway database and everything SQLite or the store
// may have put beside it, including the backup a schema upgrade takes.
func removeDB(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", ".tmp"} {
		_ = os.Remove(path + suffix)
	}
	backups, _ := filepath.Glob(path + ".pre-v*.bak")
	for _, b := range backups {
		_ = os.Remove(b)
	}
}

func overlay(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
