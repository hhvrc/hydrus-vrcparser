package hydrus

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recorded is one request the stub server received.
type recorded struct {
	Method string
	Path   string
	Query  string // raw query
	Header http.Header
	Body   string
}

// stub is an httptest server replaying canned responses in order and recording
// requests, so client tests exercise real URL construction, JSON encoding and
// transport behaviour without a live Hydrus.
type stub struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	responses []func(w http.ResponseWriter, r *http.Request)
	requests  []recorded

	// entered receives once per hang() response as the request reaches it,
	// so a test can cancel only after the request is provably in flight.
	entered chan struct{}
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{t: t, entered: make(chan struct{}, 16)}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, recorded{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: string(body),
	})
	if len(s.responses) == 0 {
		s.mu.Unlock()
		s.t.Errorf("no canned response left for %s %s", r.Method, r.URL)
		http.Error(w, "no canned response", http.StatusTeapot)
		return
	}
	respond := s.responses[0]
	s.responses = s.responses[1:]
	s.mu.Unlock()
	respond(w, r)
}

func (s *stub) json(body string) *stub {
	return s.status(http.StatusOK, body)
}

func (s *stub) status(code int, body string) *stub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	})
	return s
}

// hang never answers; it signals entered, then waits until the client
// abandons the request.
func (s *stub) hang() *stub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, func(_ http.ResponseWriter, r *http.Request) {
		s.entered <- struct{}{}
		<-r.Context().Done()
	})
	return s
}

// dropConnection closes the connection without a response: a transport error.
func (s *stub) dropConnection() *stub {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	return s
}

func (s *stub) reqs() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

// delays records backoff waits instead of sleeping.
type delays struct {
	mu sync.Mutex
	d  []time.Duration
}

func (d *delays) sleep(ctx context.Context, dur time.Duration) error {
	d.mu.Lock()
	d.d = append(d.d, dur)
	d.mu.Unlock()
	return ctx.Err()
}

func (d *delays) all() []time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]time.Duration(nil), d.d...)
}

// newTestClient points a client at s, recording backoffs rather than waiting.
func newTestClient(t *testing.T, s *stub, opts Options) (*HTTPClient, *delays) {
	t.Helper()
	opts.Address = s.srv.URL + "/"
	if opts.APIKey == "" {
		opts.APIKey = "test-key"
	}
	opts.Logger = slog.New(slog.DiscardHandler)
	c, err := NewHTTPClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	d := &delays{}
	c.sleep = d.sleep
	return c, d
}
