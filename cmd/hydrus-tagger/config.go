package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hhvrc/hydrus-vrcparser/internal/hydrus"
)

// config is everything a run needs, merged from (lowest to highest priority)
// built-in defaults, the config file, environment variables, and flags.
//
// The API key is never written anywhere by this tool. Keep it in the config
// file (outside the repository) or in HYDRUS_API_KEY.
type config struct {
	HydrusAddress string `json:"hydrus_address"`
	APIKey        string `json:"api_key"`
	ServiceName   string `json:"service_name"`
	Database      string `json:"database"`

	// DataDirectory is the Hydrus files directory, used for VRChat files
	// that have no directory recorded. No default: it is machine-specific.
	DataDirectory string `json:"data_directory"`

	// Selector overrides. Empty means the tagger's built-in default.
	VrchatSelector  []string `json:"vrchat_selector"`
	TwitterSelector []string `json:"twitter_selector"`
	TwitterNS       string   `json:"twitter_namespace"`

	Timeout duration `json:"timeout"`

	// MaxRetries is retries after a failed request; 0 disables them.
	MaxRetries int `json:"max_retries"`
}

func defaultConfig() config {
	return config{
		HydrusAddress: hydrus.DefaultAddress,
		Database:      "vrchat.db",
		Timeout:       duration(hydrus.DefaultTimeout),
		MaxRetries:    hydrus.DefaultMaxRetries,
	}
}

// defaultConfigPath is %APPDATA%\hydrus-tagger\config.json on Windows, the
// XDG equivalent elsewhere: per user, and never inside a repository.
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "hydrus-tagger", "config.json")
}

// loadConfig reads path over the defaults. A missing file is fine unless the
// path was given explicitly.
func loadConfig(path string, explicit bool) (config, error) {
	cfg := defaultConfig()
	if path == "" {
		return cfg, nil
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && !explicit {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	// Unknown keys are errors: a misspelled or old-style key (the Python
	// tool's "data_dir", "db") would otherwise be ignored, and the run would
	// quietly start over on a fresh database with default settings.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// loadSettings is the shared start of every command: the config file (the
// --config flag, else the default path) over the defaults, then the
// environment. It returns the path it read, for messages.
func loadSettings(configFlag string) (config, string, error) {
	path := firstNonEmpty(configFlag, defaultConfigPath())
	cfg, err := loadConfig(path, configFlag != "")
	if err != nil {
		return cfg, path, usageError{err}
	}
	cfg.applyEnv()
	return cfg, path, nil
}

// requireAPIKey is the check every command that talks to Hydrus makes before
// doing anything else.
func (c config) requireAPIKey(path string) error {
	if c.APIKey == "" {
		return usageError{fmt.Errorf(
			"no Hydrus API key: set HYDRUS_API_KEY, or put {\"api_key\": \"...\"} in %s "+
				"(Hydrus issues keys under services > review services > client api)", path)}
	}
	return nil
}

// newClient builds the Hydrus client from the settings.
func (c config) newClient(log *slog.Logger) (*hydrus.HTTPClient, error) {
	client, err := hydrus.NewHTTPClient(hydrus.Options{
		Address:    c.HydrusAddress,
		APIKey:     c.APIKey,
		Timeout:    time.Duration(c.Timeout),
		MaxRetries: c.MaxRetries,
		Logger:     log,
	})
	if err != nil {
		return nil, asUsage(err)
	}
	return client, nil
}

// noArgs rejects positional arguments. The flag package stops parsing at the
// first one, so in "run vrchat --dry-run" the --dry-run would otherwise be
// silently dropped -- and the run would push for real.
func noArgs(fs *flag.FlagSet) error {
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("%s takes no arguments, got %q (flags go before any other words)", fs.Name(), fs.Args())}
	}
	return nil
}

// applyEnv overlays environment variables. HYDRUS__APIKEY is the name an
// earlier version read, still accepted so an existing setup keeps working.
func (c *config) applyEnv() {
	for _, name := range []string{"HYDRUS__APIKEY", "HYDRUS_API_KEY"} {
		if v := os.Getenv(name); v != "" {
			c.APIKey = v
		}
	}
	if v := os.Getenv("HYDRUS_ADDRESS"); v != "" {
		c.HydrusAddress = v
	}
}

// duration reads "2m"-style strings from JSON.
type duration time.Duration

func (d *duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"2m\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = duration(v)
	return nil
}
