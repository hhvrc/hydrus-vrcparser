package hydrus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

// HTTPClient talks to the Hydrus client API over HTTP. Safe for concurrent use.
type HTTPClient struct {
	base       string // address with exactly one trailing slash
	apiKey     string
	timeout    time.Duration
	maxRetries int
	log        *slog.Logger
	http       *http.Client

	// sleep waits out a retry backoff, returning early with ctx's error if the
	// caller gives up. Replaced in tests so they need not actually wait.
	sleep func(ctx context.Context, d time.Duration) error
}

var _ Client = (*HTTPClient)(nil)

// NewHTTPClient validates opts and returns a client. It makes no request.
func NewHTTPClient(opts Options) (*HTTPClient, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return nil, err
	}
	return &HTTPClient{
		base:       strings.TrimRight(opts.Address, "/") + "/",
		apiKey:     opts.APIKey,
		timeout:    opts.Timeout,
		maxRetries: opts.MaxRetries,
		log:        opts.Logger,
		// No client-level timeout: each attempt carries its own deadline,
		// which covers the body read too. Redirects are not followed: the
		// client would forward the access key header to the redirect target,
		// possibly another host, so a 3xx surfaces as an *APIError instead.
		http: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		sleep: sleepCtx,
	}, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// isTransientStatus reports statuses worth retrying. Hydrus is usually a
// process on the same machine or LAN, so failures are typically "the client is
// busy importing" rather than genuine outages -- worth a few retries before
// failing a batch that may represent thousands of files. Other 4xx are never
// retried: a bad API key will not get better, and failing fast keeps the error
// immediate.
func isTransientStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// attemptOutcome is one attempt's result: either a response (status + body,
// fully read) or a failure.
type attemptOutcome struct {
	status  int
	body    []byte
	err     error
	timeout bool
}

// do sends a request with retries and returns the body of a 2xx response.
//
// Each attempt gets its own deadline derived from ctx. That is what lets a
// timed-out attempt be told apart from the caller giving up: if ctx itself is
// done, its error is returned as is and nothing is retried; otherwise a
// deadline hit is this attempt timing out, which is retried and, once retries
// run out, reported as a *ConnectionError -- never as a context error, which
// the host would read as "stop the whole run".
func (c *HTTPClient) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		out := c.attempt(ctx, method, path, body)
		if out.err == nil && out.status >= 200 && out.status < 300 {
			return out.body, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		if out.err == nil {
			if !isTransientStatus(out.status) || attempt >= c.maxRetries {
				return nil, &APIError{StatusCode: out.status, Body: string(out.body), Path: trimQuery(path)}
			}
		} else if attempt >= c.maxRetries {
			return nil, &ConnectionError{Address: c.base, Timeout: out.timeout, Err: out.err}
		}

		backoff := time.Duration(250*math.Pow(2, float64(attempt))) * time.Millisecond
		reason := fmt.Sprint(out.status)
		if out.err != nil {
			reason = out.err.Error()
		}
		c.log.Warn("Hydrus request failed; retrying",
			"path", path, "reason", reason, "retry", attempt+1, "max", c.maxRetries, "delay", backoff)

		if err := c.sleep(ctx, backoff); err != nil {
			return nil, err
		}
	}
}

func (c *HTTPClient) attempt(ctx context.Context, method, path string, body []byte) attemptOutcome {
	actx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(actx, method, c.base+path, reader)
	if err != nil {
		return attemptOutcome{err: err}
	}
	req.Header.Set(AccessKeyHeader, c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return c.failure(ctx, actx, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// The status is what matters for an error response; a body
			// that could not be read just goes unreported.
			return attemptOutcome{status: resp.StatusCode}
		}
		return c.failure(ctx, actx, err)
	}
	return attemptOutcome{status: resp.StatusCode, body: data}
}

// failure classifies a transport error. A deadline on the attempt context
// while the caller's is still live is this attempt timing out.
func (c *HTTPClient) failure(ctx, actx context.Context, err error) attemptOutcome {
	if ctx.Err() == nil && errors.Is(actx.Err(), context.DeadlineExceeded) {
		// Deliberately not wrapping err: it wraps context.DeadlineExceeded,
		// and errors.Is on that must not make a timeout look like the caller
		// giving up.
		return attemptOutcome{
			err:     fmt.Errorf("Hydrus did not respond within %s", c.timeout),
			timeout: true,
		}
	}
	return attemptOutcome{err: err}
}

// getJSON GETs path and decodes the response into v.
func (c *HTTPClient) getJSON(ctx context.Context, path string, v any) error {
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding Hydrus response for %s: %w", trimQuery(path), err)
	}
	return nil
}

func trimQuery(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

// escapeDataString percent-encodes everything except RFC 3986 unreserved
// characters (the same set as .NET's Uri.EscapeDataString). url.QueryEscape
// would turn spaces into '+', which Hydrus accepts, but strict percent-encoding
// keeps request URLs unambiguous.
func escapeDataString(s string) string {
	const hex = "0123456789ABCDEF"
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if 'A' <= b && b <= 'Z' || 'a' <= b && b <= 'z' || '0' <= b && b <= '9' ||
			b == '-' || b == '.' || b == '_' || b == '~' {
			sb.WriteByte(b)
			continue
		}
		sb.WriteByte('%')
		sb.WriteByte(hex[b>>4])
		sb.WriteByte(hex[b&0x0F])
	}
	return sb.String()
}
