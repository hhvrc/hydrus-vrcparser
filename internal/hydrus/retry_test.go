package hydrus

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRetriesATransientStatusThenSucceeds(t *testing.T) {
	s := newStub(t).
		status(http.StatusServiceUnavailable, "").
		status(http.StatusServiceUnavailable, "").
		json(`{ "file_ids": [5] }`)
	c, d := newTestClient(t, s, Options{MaxRetries: 3})

	ids, err := c.SearchFileIDs(context.Background(), []string{"a"})
	if err != nil || !slices.Equal(ids, []int{5}) {
		t.Fatalf("ids = %v, err = %v", ids, err)
	}
	if n := len(s.reqs()); n != 3 {
		t.Fatalf("%d requests", n)
	}
	if got, want := d.all(), []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}; !slices.Equal(got, want) {
		t.Fatalf("delays = %v, want %v", got, want)
	}
}

func TestEveryTransientStatusIsRetried(t *testing.T) {
	for _, code := range []int{408, 429, 500, 502, 503, 504} {
		s := newStub(t).status(code, "").json(`{}`)
		c, d := newTestClient(t, s, Options{MaxRetries: DefaultMaxRetries})
		if _, err := c.SearchFileIDs(context.Background(), []string{"a"}); err != nil {
			t.Errorf("%d: %v", code, err)
		}
		if len(d.all()) != 1 {
			t.Errorf("%d: not retried", code)
		}
	}
}

func TestReturnsTheLastTransientStatusAfterExhaustingRetries(t *testing.T) {
	s := newStub(t).
		status(http.StatusBadGateway, "busy").
		status(http.StatusBadGateway, "busy").
		status(http.StatusBadGateway, "still busy")
	c, d := newTestClient(t, s, Options{MaxRetries: 2})

	_, err := c.LocalTagServices(context.Background())

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway || apiErr.Body != "still busy" {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if len(s.reqs()) != 3 || len(d.all()) != 2 {
		t.Fatalf("%d requests, %d delays", len(s.reqs()), len(d.all()))
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 422} {
		s := newStub(t).status(code, "")
		c, d := newTestClient(t, s, Options{MaxRetries: 3})

		_, err := c.LocalTagServices(context.Background())

		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != code {
			t.Errorf("%d: err = %v", code, err)
		}
		if len(s.reqs()) != 1 || len(d.all()) != 0 {
			t.Errorf("%d: retried", code)
		}
	}
}

func TestRetriesConnectionFailuresAndReportsThemWhenExhausted(t *testing.T) {
	s := newStub(t).dropConnection().dropConnection().dropConnection()
	c, d := newTestClient(t, s, Options{MaxRetries: 2})

	_, err := c.LocalTagServices(context.Background())

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || connErr.Timeout {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if len(s.reqs()) != 3 || len(d.all()) != 2 {
		t.Fatalf("%d requests, %d delays", len(s.reqs()), len(d.all()))
	}
}

func TestPostsAreRetriedWithTheirBody(t *testing.T) {
	s := newStub(t).status(http.StatusServiceUnavailable, "").json(`{}`)
	c, _ := newTestClient(t, s, Options{MaxRetries: DefaultMaxRetries})

	if err := c.AddTags(context.Background(), []string{"h"}, "k", []string{"t"}); err != nil {
		t.Fatal(err)
	}
	reqs := s.reqs()
	if len(reqs) != 2 || reqs[0].Body == "" || reqs[0].Body != reqs[1].Body {
		t.Fatalf("requests: %+v", reqs)
	}
}

func TestAnAttemptThatTimesOutIsRetried(t *testing.T) {
	// The busy-Hydrus case: one slow attempt should cost a retry, not the batch.
	s := newStub(t).hang().json(`{ "file_ids": [] }`)
	c, d := newTestClient(t, s, Options{MaxRetries: 2, Timeout: 50 * time.Millisecond})

	if _, err := c.SearchFileIDs(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if len(s.reqs()) != 2 || len(d.all()) != 1 {
		t.Fatalf("%d requests, %d delays", len(s.reqs()), len(d.all()))
	}
}

func TestExhaustedTimeoutsAreAConnectionErrorNotACancellation(t *testing.T) {
	// The host treats context cancellation as "stop the whole run"; a busy
	// Hydrus must instead fail just the batch.
	s := newStub(t).hang().hang()
	c, _ := newTestClient(t, s, Options{MaxRetries: 1, Timeout: 50 * time.Millisecond})

	err := c.AddTags(context.Background(), []string{"h"}, "k", []string{"t"})

	var connErr *ConnectionError
	if !errors.As(err, &connErr) || !connErr.Timeout {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout reads as a context error: %v", err)
	}
}

func TestCallerCancellationIsNotRetried(t *testing.T) {
	s := newStub(t).hang()
	c, d := newTestClient(t, s, Options{MaxRetries: 3})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-s.entered
		cancel()
	}()

	_, err := c.LocalTagServices(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) {
		t.Fatalf("cancellation reported as a connection error: %v", err)
	}
	if len(s.reqs()) != 1 || len(d.all()) != 0 {
		t.Fatalf("%d requests, %d delays", len(s.reqs()), len(d.all()))
	}
}

// expiringCtx is a caller context whose deadline the test expires by hand, so
// it can be made to pass exactly while a request is in flight rather than on a
// timer that may fire before the request is sent.
type expiringCtx struct {
	context.Context // done when expire is called
	expire          context.CancelFunc
}

func newExpiringCtx() *expiringCtx {
	ctx, cancel := context.WithCancel(context.Background())
	return &expiringCtx{Context: ctx, expire: cancel}
}

func (c *expiringCtx) Err() error {
	if c.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}

func TestACallerDeadlineIsTheCallersErrorNotARetriedTimeout(t *testing.T) {
	// The caller's own deadline expiring is the caller giving up, even though
	// it looks like a timeout.
	s := newStub(t).hang()
	c, d := newTestClient(t, s, Options{MaxRetries: 3, Timeout: time.Minute})
	ctx := newExpiringCtx()
	defer ctx.expire()
	go func() {
		<-s.entered
		ctx.expire()
	}()

	_, err := c.LocalTagServices(ctx)

	if !errors.Is(err, context.DeadlineExceeded) || len(s.reqs()) != 1 || len(d.all()) != 0 {
		t.Fatalf("err = %v, %d requests, %d delays", err, len(s.reqs()), len(d.all()))
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) {
		t.Fatalf("caller deadline reported as a connection error: %v", err)
	}
}

func TestCancellationDuringBackoffStopsRetrying(t *testing.T) {
	s := newStub(t).status(http.StatusServiceUnavailable, "")
	c, _ := newTestClient(t, s, Options{MaxRetries: 3})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sleeps := 0
	c.sleep = func(ctx context.Context, _ time.Duration) error {
		// Cancel from inside the backoff, then wait it out for real: the
		// wait must end early with the caller's error.
		sleeps++
		cancel()
		return sleepCtx(ctx, time.Hour)
	}

	_, err := c.LocalTagServices(ctx)

	if !errors.Is(err, context.Canceled) || len(s.reqs()) != 1 || sleeps != 1 {
		t.Fatalf("err = %v, %d requests, %d sleeps", err, len(s.reqs()), sleeps)
	}
}
