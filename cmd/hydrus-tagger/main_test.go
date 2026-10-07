package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigFileOverridesDefaults(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, `{"api_key": "k", "service_name": "my tags", "timeout": "30s", "max_retries": 5}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "k" || cfg.ServiceName != "my tags" || time.Duration(cfg.Timeout) != 30*time.Second || cfg.MaxRetries != 5 {
		t.Fatalf("cfg = %+v", cfg)
	}
	// Untouched settings keep their defaults.
	if cfg.HydrusAddress != "http://127.0.0.1:45869" || cfg.Database != "vrchat.db" {
		t.Fatalf("defaults lost: %+v", cfg)
	}
}

func TestAMissingDefaultConfigIsFineButAMissingExplicitOneIsNot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")
	if _, err := loadConfig(missing, false); err != nil {
		t.Fatalf("default path missing should be fine: %v", err)
	}
	if _, err := loadConfig(missing, true); err == nil {
		t.Fatal("explicit --config that does not exist should fail")
	}
}

func TestABadConfigIsReported(t *testing.T) {
	for _, body := range []string{`{not json`, `{"timeout": 30}`, `{"timeout": "soon"}`} {
		if _, err := loadConfig(writeConfig(t, body), true); err == nil {
			t.Errorf("%s: want an error", body)
		}
	}
}

func TestEnvironmentOverridesTheConfigFile(t *testing.T) {
	cfg, _ := loadConfig(writeConfig(t, `{"api_key": "from-file", "hydrus_address": "http://file:1"}`), true)
	t.Setenv("HYDRUS__APIKEY", "")
	t.Setenv("HYDRUS_API_KEY", "from-env")
	t.Setenv("HYDRUS_ADDRESS", "http://env:2")

	cfg.applyEnv()

	if cfg.APIKey != "from-env" || cfg.HydrusAddress != "http://env:2" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestTheCSharpToolsVariableStillWorks(t *testing.T) {
	cfg := defaultConfig()
	t.Setenv("HYDRUS_API_KEY", "")
	t.Setenv("HYDRUS__APIKEY", "legacy")
	cfg.applyEnv()
	if cfg.APIKey != "legacy" {
		t.Fatalf("api key = %q", cfg.APIKey)
	}
}

func TestFlagsOverrideOnlyWhenGiven(t *testing.T) {
	v := "from-config"
	overlay(&v, "")
	if v != "from-config" {
		t.Fatalf("empty flag overwrote config: %q", v)
	}
	overlay(&v, "from-flag")
	if v != "from-flag" {
		t.Fatalf("flag ignored: %q", v)
	}
}

func TestOnlyAcceptsRepeatedAndCommaSeparatedValues(t *testing.T) {
	var l listFlag
	l.Set("vrchat, twitter")
	l.Set("other")
	l.Set(" , ")
	if !slices.Equal(l, listFlag{"vrchat", "twitter", "other"}) {
		t.Fatalf("list = %v", l)
	}
}

func TestExitCodes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{context.Canceled, 130},
		{usageError{errors.New("no key")}, 2},
		{asUsage(&hydrus.ConnectionError{Address: "x", Err: errors.New("refused")}), 2},
		{asUsage(&hydrus.ServiceResolutionError{Msg: "ambiguous"}), 2},
		{errors.New("one or more taggers failed"), 1},
	}
	for _, c := range cases {
		if got := exitCode(c.err, log); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestRunWithoutAnAPIKeyNamesTheConfigFileAndTouchesNothing(t *testing.T) {
	t.Setenv("HYDRUS_API_KEY", "")
	t.Setenv("HYDRUS__APIKEY", "")
	dir := t.TempDir()
	db := filepath.Join(dir, "vrchat.db")
	cfg := writeConfig(t, `{}`)

	if code := runCommand(context.Background(), []string{"--config", cfg, "--db", db}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatal("a run without a key created the database")
	}
}

func TestUnknownCommandsAndBareInvocationAreUsageErrors(t *testing.T) {
	if code := realMain(nil); code != 2 {
		t.Errorf("no args: %d", code)
	}
	if code := realMain([]string{"frobnicate"}); code != 2 {
		t.Errorf("unknown command: %d", code)
	}
	if code := realMain([]string{"parity", "chunks"}); code != 2 {
		t.Errorf("parity with missing args: %d", code)
	}
}

func TestStatusReportsAMissingDatabaseInsteadOfCreatingOne(t *testing.T) {
	db := filepath.Join(t.TempDir(), "absent.db")
	if code := statusCommand(context.Background(), []string{"--config", writeConfig(t, `{}`), "--db", db}); code != 2 {
		t.Fatalf("exit code = %d", code)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatal("status created a database")
	}
}

func TestRemoveDBCleansUpTheSnapshotAndItsSidecars(t *testing.T) {
	base := filepath.Join(t.TempDir(), "snap.db")
	for _, suffix := range []string{"", "-wal", "-shm", ".tmp"} {
		os.WriteFile(base+suffix, nil, 0o600)
	}
	removeDB(base)
	matches, _ := filepath.Glob(base + "*")
	if len(matches) != 0 {
		t.Fatalf("left behind: %v", matches)
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	for _, cmd := range []string{"run", "status", "correlate", "parity"} {
		if !strings.Contains(usage, "hydrus-tagger "+cmd) {
			t.Errorf("usage does not mention %s", cmd)
		}
	}
}
