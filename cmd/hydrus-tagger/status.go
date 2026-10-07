package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hhvrc/hydrus-vrcparser/internal/store"
)

// statusCommand prints a summary of the cache database. Read-only, and needs
// no Hydrus connection.
func statusCommand(ctx context.Context, args []string) int {
	log := newLogger()

	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	configPath := fs.String("config", "", "config file (default "+defaultConfigPath()+")")
	dbPath := fs.String("db", "", "cache database (default from config, else vrchat.db)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := noArgs(fs); err != nil {
		return exitCode(err, log)
	}

	cfg, _, err := loadSettings(*configPath)
	if err != nil {
		return exitCode(err, log)
	}
	overlay(&cfg.Database, *dbPath)

	path, err := filepath.Abs(cfg.Database)
	if err != nil {
		return exitCode(err, log)
	}
	if _, err := os.Stat(path); err != nil {
		return exitCode(usageError{fmt.Errorf("no database at %s", path)}, log)
	}

	db, err := store.OpenReadOnly(ctx, path)
	if err != nil {
		return exitCode(err, log)
	}
	defer db.Close()

	s, err := db.Status(ctx)
	if err != nil {
		return exitCode(err, log)
	}
	printStatus(path, s)
	return 0
}

// printStatus writes the report. ASCII only: Windows consoles have crashed on
// anything else (the original Python diagnostics did, on an emoji).
func printStatus(path string, s *store.Status) {
	fmt.Printf("Database        %s\n", path)
	fmt.Printf("Schema version  %d\n\n", s.SchemaVersion)

	fmt.Printf("Files           %d (%d found on disk by the legacy pipeline)\n", s.Files, s.FilesWithDataDir)
	fmt.Printf("iTXt chunks     %d across %d files\n", s.Chunks, s.FilesWithChunks)
	for _, c := range s.ChunksByType {
		fmt.Printf("  %-14s %d\n", c.Label, c.N)
	}

	if len(s.UnparseableChunks) > 0 {
		fmt.Println("\nChunks under VRChat keywords that parse as no known format:")
		for _, c := range s.UnparseableChunks {
			fmt.Printf("  %-25s %d\n", c.Label, c.N)
		}
	}

	for _, t := range s.Taggers {
		fmt.Printf("\nTagger %s\n", t.ID)
		fmt.Println("  files by extract/derive version:")
		for _, v := range t.Versions {
			fmt.Printf("    %-10s %d\n", v.Label, v.N)
		}
		fmt.Printf("  tag sets stored     %d (%d empty)\n", t.TagSets, t.EmptyTagSets)
		fmt.Printf("  pushed              %d\n", t.Pushed)
		fmt.Printf("  waiting to push     %d\n", t.Unpushed)
		if t.ExtractedWithoutChunks > 0 {
			fmt.Printf("  read, no iTXt       %d\n", t.ExtractedWithoutChunks)
		}
	}
}
