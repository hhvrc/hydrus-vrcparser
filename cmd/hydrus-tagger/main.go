// Command hydrus-tagger tags files in a Hydrus client from VRChat screenshot
// metadata and Twitter/X URLs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"
)

const usage = `hydrus-tagger tags Hydrus files from VRChat screenshot metadata and Twitter/X URLs.

Usage:
  hydrus-tagger run [flags]             derive tags and push them to Hydrus
  hydrus-tagger status [--db <path>]    summarize the cache database (read-only)
  hydrus-tagger correlate [flags]       suggest user-id <-> user-name pairs (read-only)
  hydrus-tagger parity chunks <db> <out.jsonl>
  hydrus-tagger parity tags   <db> <out.jsonl>

Settings come from the config file (default %APPDATA%\hydrus-tagger\config.json),
then the environment (HYDRUS_API_KEY, HYDRUS_ADDRESS), then flags.

Run "hydrus-tagger run -h" for run flags.
`

func main() {
	os.Exit(realMain(os.Args[1:]))
}

func realMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	// Ctrl+C cancels the context; the run then saves its progress and
	// returns. A second Ctrl+C kills the process outright.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	switch args[0] {
	case "run":
		return runCommand(ctx, args[1:])
	case "status":
		return statusCommand(ctx, args[1:])
	case "correlate":
		return correlateCommand(ctx, args[1:])
	case "parity":
		return parityCommand(ctx, args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Only the record's own timestamp; an attribute that happens to
			// be called "time" may hold anything.
			if a.Key == slog.TimeKey && len(groups) == 0 && a.Value.Kind() == slog.KindTime {
				return slog.String(slog.TimeKey, a.Value.Time().Format(time.TimeOnly))
			}
			return a
		},
	}))
}

// listFlag collects a flag given several times, or comma-separated.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
	return nil
}

// exitCode maps a run's error to the process exit code: 2 for configuration
// and connectivity problems the user must fix, 130 for Ctrl+C, 1 otherwise.
func exitCode(err error, log *slog.Logger) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		log.Warn("cancelled; progress so far was saved")
		return 130
	}
	var ue usageError
	if errors.As(err, &ue) {
		log.Error(ue.Error())
		return 2
	}
	log.Error(err.Error())
	return 1
}

// usageError is a problem the user fixes by changing settings, reported
// without further detail.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }
