package hydrus

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// Defaults. Address and Timeout apply when those Options fields are left zero;
// MaxRetries has no zero-value default (see Options.MaxRetries), so callers
// wanting the default pass DefaultMaxRetries.
const (
	DefaultAddress    = "http://127.0.0.1:45869"
	DefaultTimeout    = 2 * time.Minute
	DefaultMaxRetries = 3

	// DefaultMetadataBatchSize is how many file ids callers put in one
	// FileMetadata request.
	DefaultMetadataBatchSize = 256
)

// AccessKeyHeader carries the API key on every request.
const AccessKeyHeader = "Hydrus-Client-API-Access-Key"

// Options configures an HTTPClient. Zero Address and Timeout take the
// defaults; a zero MaxRetries means no retries.
type Options struct {
	// Address is the client API address, e.g. http://127.0.0.1:45869. A
	// trailing slash is tolerated (the legacy config.json had one).
	Address string

	// APIKey is the client API access key. Required.
	APIKey string

	// Timeout applies to each attempt, body read included; an attempt that
	// times out is retried.
	Timeout time.Duration

	// MaxRetries is how many times a transient failure is retried after the
	// first attempt. Zero disables retries; negative is a ConfigError. Zero
	// does not mean "default" because a configured max_retries of 0 must mean
	// what it says; pass DefaultMaxRetries for the default.
	MaxRetries int

	// Logger receives retry warnings and the resolved service. Nil means
	// slog.Default().
	Logger *slog.Logger
}

// withDefaults validates o and fills in defaults.
func (o Options) withDefaults() (Options, error) {
	if strings.TrimSpace(o.APIKey) == "" {
		return o, &ConfigError{Msg: "no Hydrus API key configured: set the HYDRUS_API_KEY environment " +
			"variable or the API key in the config file (Hydrus issues keys under services > review services > client api)"}
	}

	if o.Address == "" {
		o.Address = DefaultAddress
	}
	u, err := url.Parse(o.Address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return o, &ConfigError{Msg: "invalid Hydrus address: '" + o.Address + "'"}
	}

	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxRetries < 0 {
		return o, &ConfigError{Msg: fmt.Sprintf("invalid Hydrus max retries: %d (must be 0 or more)", o.MaxRetries)}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return o, nil
}
