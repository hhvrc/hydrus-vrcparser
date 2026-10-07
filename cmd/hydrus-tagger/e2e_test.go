package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// e2e is a fake Hydrus plus a config file pointing the CLI at it, so tests can
// drive realMain exactly as a user would.
type e2e struct {
	fake    *fakeHydrus
	config  string
	db      string
	dataDir string
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	t.Setenv("HYDRUS_API_KEY", "")
	t.Setenv("HYDRUS__APIKEY", "")
	t.Setenv("HYDRUS_ADDRESS", "")

	fake := &fakeHydrus{files: map[int][]string{}, pngs: map[int]bool{}, userTags: map[int][]string{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	e := &e2e{fake: fake, db: filepath.Join(dir, "vrchat.db"), dataDir: filepath.Join(dir, "files")}
	cfg, _ := json.Marshal(map[string]any{
		"hydrus_address": srv.URL,
		"api_key":        "test-key",
		"service_name":   "my tags",
		"database":       e.db,
		"data_directory": e.dataDir,
	})
	e.config = filepath.Join(dir, "config.json")
	if err := os.WriteFile(e.config, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *e2e) main(args ...string) int {
	return realMain(append([]string{args[0], "--config", e.config}, args[1:]...))
}

// writeVRChatPNG puts a minimal PNG with one VRCX JSON iTXt chunk where the
// VRChat tagger will look for file id.
func (e *e2e) writeVRChatPNG(t *testing.T, id int, json string) {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	payload := "Description\x00\x00\x00\x00\x00" + json
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	b.Write(length[:])
	b.WriteString("iTXt" + payload)
	b.Write(make([]byte, 4)) // CRC; nothing verifies it

	hash := fakeHash(id)
	path := filepath.Join(e.dataDir, "f"+hash[:2], hash+".png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	e.fake.pngs[id] = true
}

func pushedTags(f *fakeHydrus) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, call := range f.addTags {
		for _, tags := range call["service_keys_to_tags"].(map[string]any) {
			var set []string
			for _, t := range tags.([]any) {
				set = append(set, t.(string))
			}
			out = append(out, set)
		}
	}
	return out
}

func TestAWordBeforeAFlagIsRejectedInsteadOfDroppingTheFlag(t *testing.T) {
	// The flag package stops at the first non-flag, so "run vrchat --dry-run"
	// used to run for real with --dry-run ignored.
	e := newE2E(t)
	e.fake.files[1] = []string{"https://x.com/someone/status/1"}

	if code := e.main("run", "vrchat", "--dry-run"); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if e.fake.requests != 0 || e.fake.pushes() != 0 {
		t.Fatalf("made %d requests, %d pushes", e.fake.requests, e.fake.pushes())
	}
}

func TestAnUnknownOnlyNameStopsBeforeAnythingIsTouched(t *testing.T) {
	e := newE2E(t)
	if code := e.main("run", "--only", "twiter"); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if _, err := os.Stat(e.db); !os.IsNotExist(err) {
		t.Fatal("database created for a run that could not start")
	}
	if e.fake.requests != 0 {
		t.Fatalf("contacted Hydrus %d times", e.fake.requests)
	}
}

func TestAnUnknownConfigKeyIsAnError(t *testing.T) {
	// e.g. the Python tool's "data_dir": ignoring it would quietly drop the
	// setting.
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"api_key": "k", "data_dir": "\\\\host\\files"}`), 0o600)
	if code := realMain([]string{"status", "--config", path}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestAVRChatRunExtractsTagsAndASecondRunDoesNothing(t *testing.T) {
	e := newE2E(t)
	e.writeVRChatPNG(t, 10, `{"author":{"id":"usr_a","displayName":"A"},"world":{"id":"wrld_b","name":"B","instanceId":""},"players":[]}`)

	if code := e.main("run", "--only", "vrchat"); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	pushed := pushedTags(e.fake)
	if len(pushed) != 1 || !slices.Contains(pushed[0], "vrchat-author-name:A") || !slices.Contains(pushed[0], "vrchat-world-name:B") {
		t.Fatalf("pushed = %v", pushed)
	}

	e.fake.requests = 0
	if code := e.main("run", "--only", "vrchat"); code != 0 {
		t.Fatalf("second run exit code = %d", code)
	}
	if e.fake.pushes() != 1 {
		t.Fatalf("second run pushed again: %d calls", e.fake.pushes())
	}
}

func TestStatusReadsTheDatabaseARunLeft(t *testing.T) {
	e := newE2E(t)
	e.fake.files[1] = []string{"https://x.com/someone/status/1"}
	if code := e.main("run"); code != 0 {
		t.Fatalf("run exit code = %d", code)
	}
	if code := e.main("status"); code != 0 {
		t.Fatalf("status exit code = %d", code)
	}
}

func TestCorrelateWritesItsCSVAndNeverPushes(t *testing.T) {
	e := newE2E(t)
	for id := 1; id <= 3; id++ {
		e.fake.userTags[id] = []string{"vrchat-user-id:usr_x", "vrchat-user-name:somebody"}
	}
	out := filepath.Join(t.TempDir(), "corr.csv")

	if code := e.main("correlate", "--out", out); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	csv, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// One id and one name on identical file sets are already linked, so
	// nothing is suggested: the CSV is just the header.
	want := "user_id,name,overlap,id_files,name_files,jaccard,runner_up_jaccard,example_hashes\r\n"
	if string(csv) != want {
		t.Fatalf("CSV = %q, want %q", csv, want)
	}
	if e.fake.pushes() != 0 {
		t.Fatal("correlate pushed tags")
	}
}

func TestCommandsReportAnUnreachableHydrusAsAUsageError(t *testing.T) {
	e := newE2E(t)
	cfg, _ := json.Marshal(map[string]any{"hydrus_address": "http://127.0.0.1:1", "api_key": "k", "max_retries": 0, "database": e.db})
	os.WriteFile(e.config, cfg, 0o600)

	for _, cmd := range []string{"run", "correlate"} {
		if code := e.main(cmd); code != 2 {
			t.Errorf("%s: exit code = %d, want 2", cmd, code)
		}
	}
	if _, err := os.Stat(e.db); !os.IsNotExist(err) {
		t.Fatal("database created though Hydrus was unreachable")
	}
}

func TestRemoveDBAlsoRemovesASchemaUpgradeBackup(t *testing.T) {
	base := filepath.Join(t.TempDir(), "snap.db")
	for _, name := range []string{base, base + ".pre-v2.bak"} {
		os.WriteFile(name, nil, 0o600)
	}
	removeDB(base)
	if left, _ := filepath.Glob(base + "*"); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}
