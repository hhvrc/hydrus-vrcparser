package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hhvrc/hydrus-vrcparser/internal/parity"
)

// parityCommand writes the VRChat parser's per-chunk or per-file results over
// a database as JSONL, for diffing a before and an after dump with
// tools/parity/compare_*.py. Read-only; run it on a copy anyway.
func parityCommand(ctx context.Context, args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: hydrus-tagger parity chunks|tags <db> <out.jsonl>")
		return 2
	}

	var err error
	switch args[0] {
	case "chunks":
		err = parity.DumpChunkParse(ctx, args[1], args[2])
	case "tags":
		err = parity.DumpFileTags(ctx, args[1], args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown dump %q; want chunks or tags\n", args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "parity:", err)
		return 1
	}
	return 0
}
