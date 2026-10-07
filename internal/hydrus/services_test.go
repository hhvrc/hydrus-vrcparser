package hydrus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func collect(t *testing.T, js string) []Service {
	t.Helper()
	services, err := collectLocalTagServices([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return services
}

func TestReadsModernServicesObjectKeyedByServiceKey(t *testing.T) {
	// The modern shape omits service_key inside the value; the object key is it.
	services := collect(t, `{
	  "services": {
	    "6c6f63616c2074616773": { "name": "my tags", "type": 5, "type_pretty": "local tag service" },
	    "616c6c206b6e6f776e2074616773": { "name": "all known tags", "type": 10 }
	  }
	}`)

	want := []Service{{Key: "6c6f63616c2074616773", Name: "my tags", Type: LocalTagServiceType}}
	if !slices.Equal(services, want) {
		t.Fatalf("got %+v", services)
	}
}

func TestReadsLegacyLocalTagsList(t *testing.T) {
	services := collect(t, `{ "local_tags": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ] }`)

	if want := []Service{{Key: "abc123", Name: "my tags", Type: 5}}; !slices.Equal(services, want) {
		t.Fatalf("got %+v", services)
	}
}

func TestDedupesAServiceReportedUnderSeveralTopLevelKeys(t *testing.T) {
	// Why reading a single key is not enough: real responses repeat the same
	// service under several keys.
	services := collect(t, `{
	  "local_tags": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ],
	  "services_v2": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ],
	  "services": { "abc123": { "name": "my tags", "type": 5 } }
	}`)

	if len(services) != 1 {
		t.Fatalf("got %+v", services)
	}
}

func TestKeepsFirstSeenOrder(t *testing.T) {
	services := collect(t, `{
	  "services": { "k2": { "name": "second", "type": 5 }, "k1": { "name": "first", "type": 5 } },
	  "local_tags": [ { "name": "renamed", "service_key": "k2", "type": 5 } ]
	}`)

	want := []Service{{Key: "k2", Name: "renamed", Type: 5}, {Key: "k1", Name: "first", Type: 5}}
	if !slices.Equal(services, want) {
		t.Fatalf("got %+v", services)
	}
}

func TestIgnoresNonLocalTagServices(t *testing.T) {
	services := collect(t, `{
	  "services": {
	    "a": { "name": "all known files", "type": 11 },
	    "b": { "name": "my files",        "type": 2  },
	    "c": { "name": "downloader tags", "type": 0  },
	    "d": { "name": "stringly typed",  "type": "5" },
	    "e": { "name": "fractional",      "type": 5.0 }
	  }
	}`)

	if len(services) != 0 {
		t.Fatalf("got %+v", services)
	}
}

func TestHandlesABareArrayRoot(t *testing.T) {
	services := collect(t, `[ { "name": "my tags", "service_key": "abc123", "type": 5 } ]`)

	if len(services) != 1 {
		t.Fatalf("got %+v", services)
	}
}

func TestSkipsEntriesWithNoResolvableKey(t *testing.T) {
	services := collect(t, `{
	  "local_tags": [ { "name": "keyless", "type": 5 }, { "name": "empty", "service_key": "", "type": 5 } ],
	  "services": { "k": { "name": "explicitly empty", "service_key": "", "type": 5 } }
	}`)

	if len(services) != 0 {
		t.Fatalf("got %+v", services)
	}
}

func TestIgnoresScalarAndNonObjectEntries(t *testing.T) {
	services := collect(t, `{ "version": 94, "local_tags": [ 1, "x", null ], "services": [] }`)
	if len(services) != 0 {
		t.Fatalf("got %+v", services)
	}
	if got := collect(t, `42`); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func clientWithServices(t *testing.T, js string) *HTTPClient {
	t.Helper()
	s := newStub(t).json(js)
	c, _ := newTestClient(t, s, Options{})
	return c
}

const twoServices = `{
  "services": {
    "k1": { "name": "my tags",   "type": 5 },
    "k2": { "name": "downloads", "type": 5 }
  }
}`

func TestResolvesByNameWhenSeveralServicesExist(t *testing.T) {
	key, err := clientWithServices(t, twoServices).ResolveLocalTagServiceKey(context.Background(), "downloads")
	if err != nil || key != "k2" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
}

func TestResolvesASingleServiceWithoutAName(t *testing.T) {
	c := clientWithServices(t, `{ "services": { "k1": { "name": "my tags", "type": 5 } } }`)

	key, err := c.ResolveLocalTagServiceKey(context.Background(), "")
	if err != nil || key != "k1" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
}

func TestRefusesToGuessBetweenSeveralServices(t *testing.T) {
	_, err := clientWithServices(t, twoServices).ResolveLocalTagServiceKey(context.Background(), "  ")

	var resErr *ServiceResolutionError
	if !errors.As(err, &resErr) || len(resErr.Available) != 2 || !strings.Contains(err.Error(), "my tags") {
		t.Fatalf("err = %v", err)
	}
}

func TestListsAvailableServicesWhenTheNamedOneIsMissing(t *testing.T) {
	c := clientWithServices(t, `{ "services": { "k1": { "name": "my tags", "type": 5 } } }`)

	_, err := c.ResolveLocalTagServiceKey(context.Background(), "typo tags")

	var resErr *ServiceResolutionError
	if !errors.As(err, &resErr) || !strings.Contains(err.Error(), "typo tags") || !strings.Contains(err.Error(), "my tags") {
		t.Fatalf("err = %v", err)
	}
}

func TestRefusesADuplicatedName(t *testing.T) {
	c := clientWithServices(t, `{ "services": { "k1": { "name": "same", "type": 5 }, "k2": { "name": "same", "type": 5 } } }`)

	_, err := c.ResolveLocalTagServiceKey(context.Background(), "same")

	var resErr *ServiceResolutionError
	if !errors.As(err, &resErr) || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("err = %v", err)
	}
}

func TestFailsWhenNoLocalTagServiceExists(t *testing.T) {
	c := clientWithServices(t, `{ "services": { "k1": { "name": "my files", "type": 2 } } }`)

	_, err := c.ResolveLocalTagServiceKey(context.Background(), "")

	var resErr *ServiceResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("err = %v", err)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A real get_services response captured from Hydrus 681 (API v94). It reports
// the same service under "local_tags", "services" (keyed by service key, with
// no service_key inside the value) and "services_v2" at once, so it exercises
// the dedup and the object-key fallback against the genuine article.
func TestMatchesRealHydrusResponse(t *testing.T) {
	services := collect(t, readFixture(t, "get_services.json"))

	// Two local tag services exist on this client; 12 services in total.
	var names []string
	for _, s := range services {
		if s.Type != LocalTagServiceType || s.Key == "" {
			t.Errorf("bad service %+v", s)
		}
		names = append(names, s.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"my tags", "pixai tags"}) {
		t.Fatalf("names = %q", names)
	}
}

func TestRealResponseRequiresAnExplicitServiceName(t *testing.T) {
	// This client has two local tag services, so the no-name path must refuse
	// rather than silently pick one.
	_, err := clientWithServices(t, readFixture(t, "get_services.json")).ResolveLocalTagServiceKey(context.Background(), "")

	var resErr *ServiceResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolvesTheConfiguredServiceFromTheRealResponse(t *testing.T) {
	// "my tags" is the service the legacy config.json targets.
	key, err := clientWithServices(t, readFixture(t, "get_services.json")).ResolveLocalTagServiceKey(context.Background(), "my tags")
	if err != nil || key != "6c6f63616c2074616773" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
}
