package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/hhvrc/hydrus-vrcparser/internal/correlate"
	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
)

// correlateSearch finds every file carrying a user id or a user name tag.
//
// It searches "all known tags" rather than one tag service: extraction below
// aggregates current tags across every service anyway, so a file found
// through any service contributes the same ids and names either way.
const correlateSearch = correlate.IDPrefix + "* OR " + correlate.NamePrefix + "*"

// correlateCommand infers which
// vrchat-user-id tags belong with which vrchat-user-name tags from how their
// file sets overlap, and reports the suggestions on the console and as CSV.
// Read-only: it never writes tags to Hydrus.
func correlateCommand(ctx context.Context, args []string) int {
	log := newLogger()

	defaults := correlate.DefaultOptions()
	fs := flag.NewFlagSet("correlate", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file (default "+defaultConfigPath()+")")
	addr := fs.String("hydrus-addr", "", "Hydrus client API address")
	out := fs.String("out", "user_correlations.csv", "CSV output path")
	minOverlap := fs.Int("min-overlap", defaults.MinOverlap,
		"min files an id and name must share to be suggested")
	minJaccard := fs.Float64("min-jaccard", defaults.MinJaccard,
		"min file-set Jaccard similarity to suggest a pair")
	maxRunnerUp := fs.Float64("max-runner-up-ratio", defaults.MaxRunnerUpRatio,
		"reject if the 2nd-best name scores above this fraction of the winner")
	show := fs.Int("show", 50, "max suggestion rows to print")
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
	overlay(&cfg.HydrusAddress, *addr)

	opts := correlate.Options{MinOverlap: *minOverlap, MinJaccard: *minJaccard, MaxRunnerUpRatio: *maxRunnerUp}
	return exitCode(runCorrelate(ctx, log, cfg, opts, *out, *show), log)
}

func runCorrelate(ctx context.Context, log *slog.Logger, cfg config, opts correlate.Options, out string, show int) error {
	client, err := cfg.newClient(log)
	if err != nil {
		return err
	}

	log.Info("searching Hydrus for user-id / user-name tagged files")
	fileIDs, err := client.SearchFileIDs(ctx, []string{correlateSearch})
	if err != nil {
		return asUsage(err)
	}
	if len(fileIDs) == 0 {
		fmt.Println("No files with vrchat-user-id:* or vrchat-user-name:* tags found.")
		return nil
	}
	fileIDs = slices.Clone(fileIDs)
	slices.Sort(fileIDs)
	fileIDs = slices.Compact(fileIDs)

	log.Info("fetching metadata", "files", len(fileIDs))
	files, index, err := fetchCorrelationFiles(ctx, log, client, fileIDs)
	if err != nil {
		return asUsage(err)
	}

	log.Info("correlating")
	result := correlate.Correlate(files, opts)

	if err := writeCorrelationCSV(out, result.Suggestions, index); err != nil {
		return err
	}
	correlate.WriteReport(os.Stdout, result, show, out)
	return nil
}

// fetchCorrelationFiles fetches metadata in batches and returns each file's
// ids and names, skipping files with neither, plus the hash index the CSV
// cites example screenshots from.
func fetchCorrelationFiles(
	ctx context.Context, log *slog.Logger, client hydrus.Client, fileIDs []int,
) ([]correlate.File, *correlate.HashIndex, error) {
	var files []correlate.File
	index := correlate.NewHashIndex()

	batches := (len(fileIDs) + hydrus.DefaultMetadataBatchSize - 1) / hydrus.DefaultMetadataBatchSize
	n := 0
	for batch := range slices.Chunk(fileIDs, hydrus.DefaultMetadataBatchSize) {
		n++
		rows, err := client.FileMetadata(ctx, batch)
		if err != nil {
			return nil, nil, err
		}
		for i := range rows {
			f := correlate.FromTags(rows[i].CurrentTags())
			if f.Empty() {
				continue
			}
			files = append(files, f)
			index.Add(rows[i].Hash, f)
		}
		log.Info("fetched metadata batch", "batch", n, "of", batches)
	}
	return files, index, nil
}

func writeCorrelationCSV(path string, suggestions []correlate.Suggestion, index *correlate.HashIndex) error {
	f, err := os.Create(path)
	if err != nil {
		return usageError{fmt.Errorf("write CSV: %w", err)}
	}
	werr := correlate.WriteCSV(f, suggestions, index)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		return fmt.Errorf("write CSV %s: %w", path, err)
	}
	return nil
}
