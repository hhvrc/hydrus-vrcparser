package hydrus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// LocalTagServices returns every local tag service (type 5), deduped by
// service key.
func (c *HTTPClient) LocalTagServices(ctx context.Context) ([]Service, error) {
	data, err := c.do(ctx, http.MethodGet, "get_services", nil)
	if err != nil {
		return nil, err
	}
	services, err := collectLocalTagServices(data)
	if err != nil {
		return nil, fmt.Errorf("decoding Hydrus response for get_services: %w", err)
	}
	return services, nil
}

// collectLocalTagServices reads local tag services from a get_services
// response.
//
// The same service is reported under several top-level keys depending on
// Hydrus version ("local_tags", "services", "services_v2"), so every
// list-valued key is scanned, *and* the modern "services" object keyed by
// service key, deduping by key. Reading only "local_tags" would silently miss
// services on newer clients.
//
// Order is first-seen; a later sighting of the same key replaces the earlier
// one in place.
func collectLocalTagServices(data []byte) ([]Service, error) {
	var out []Service
	index := make(map[string]int)

	consider := func(raw json.RawMessage, fallbackKey string, hasFallback bool) {
		var svc map[string]json.RawMessage
		if !isJSONObject(raw) || json.Unmarshal(raw, &svc) != nil {
			return
		}

		// Integral numbers only: 5.0 or "5" is not a service type.
		typ, err := strconv.ParseInt(string(bytes.TrimSpace(svc["type"])), 10, 32)
		if err != nil || typ != LocalTagServiceType {
			return
		}

		// A service_key present as a string wins, even if empty (and then the
		// entry is skipped); only an absent or non-string one falls back.
		key, ok := jsonString(svc["service_key"])
		if !ok {
			key, ok = fallbackKey, hasFallback
		}
		if !ok || key == "" {
			return
		}

		name, _ := jsonString(svc["name"])
		s := Service{Key: key, Name: name, Type: int(typ)}
		if i, seen := index[key]; seen {
			out[i] = s
			return
		}
		index[key] = len(out)
		out = append(out, s)
	}

	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		for _, item := range items {
			consider(item, "", false)
		}
		return out, nil

	case len(trimmed) > 0 && trimmed[0] == '{':
		err := eachMember(trimmed, func(name string, value json.RawMessage) error {
			v := bytes.TrimSpace(value)
			switch {
			case len(v) > 0 && v[0] == '[':
				var items []json.RawMessage
				if err := json.Unmarshal(v, &items); err != nil {
					return err
				}
				for _, item := range items {
					consider(item, "", false)
				}
			case name == "services" && len(v) > 0 && v[0] == '{':
				// Modern shape: an object keyed by service_key, where the
				// value may omit service_key itself.
				return eachMember(v, func(key string, svc json.RawMessage) error {
					consider(svc, key, true)
					return nil
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil

	default:
		if !json.Valid(trimmed) {
			return nil, errors.New("invalid JSON")
		}
		return nil, nil
	}
}

// eachMember walks a JSON object's members in document order, which a map
// would lose.
func eachMember(obj []byte, fn func(name string, value json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil { // '{'
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return err
		}
		if err := fn(name, value); err != nil {
			return err
		}
	}
	_, err := dec.Token() // '}'
	return err
}

func isJSONObject(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	return len(v) > 0 && v[0] == '{'
}

// jsonString returns raw's value if it is a JSON string.
func jsonString(raw json.RawMessage) (string, bool) {
	v := bytes.TrimSpace(raw)
	if len(v) == 0 || v[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", false
	}
	return s, true
}

func describeService(s Service) string {
	name := s.Name
	if name == "" {
		name = "?"
	}
	return fmt.Sprintf("%s (key=%s)", name, s.Key)
}

// ResolveLocalTagServiceKey implements Client. With a name, exactly one local
// tag service must have it (names match exactly); without one, exactly one
// local tag service must exist, so a client with several never has one picked
// silently.
func (c *HTTPClient) ResolveLocalTagServiceKey(ctx context.Context, name string) (string, error) {
	services, err := c.LocalTagServices(ctx)
	if err != nil {
		return "", err
	}
	if len(services) == 0 {
		return "", &ServiceResolutionError{Msg: "no local tag service (type 5) found"}
	}

	available := make([]string, len(services))
	for i, s := range services {
		available[i] = describeService(s)
	}

	var chosen Service
	if strings.TrimSpace(name) != "" {
		var matches []Service
		for _, s := range services {
			if s.Name == name {
				matches = append(matches, s)
			}
		}
		switch len(matches) {
		case 0:
			return "", &ServiceResolutionError{Msg: fmt.Sprintf("no local tag service named '%s'", name), Available: available}
		case 1:
			chosen = matches[0]
		default:
			return "", &ServiceResolutionError{Msg: fmt.Sprintf("multiple local tag services named '%s'", name), Available: available}
		}
	} else {
		if len(services) > 1 {
			return "", &ServiceResolutionError{
				Msg:       "multiple local tag services found; specify which to use",
				Available: available,
			}
		}
		chosen = services[0]
	}

	c.log.Info("Using local tag service", "service", describeService(chosen))
	return chosen.Key, nil
}

// orSeparator splits a search term into OR alternatives. Hydrus lowercases
// every tag, so an uppercase " OR " cannot occur inside a real one. That lets a
// selector stay a flat list of strings.
const orSeparator = " OR "

// serializeSearchTags encodes search terms as the JSON Hydrus expects,
// expanding "a OR b" into the nested list of an OR predicate.
//
// A term containing " OR " that yields fewer than two non-empty alternatives
// is an error. Passing it through would search for a literal tag containing
// " OR ", which cannot exist, and silently match nothing.
func serializeSearchTags(tags []string) (string, error) {
	terms := make([]any, 0, len(tags))
	for _, tag := range tags {
		if !strings.Contains(tag, orSeparator) {
			terms = append(terms, tag)
			continue
		}
		var alternatives []string
		for _, part := range strings.Split(tag, orSeparator) {
			if part = strings.TrimSpace(part); part != "" {
				alternatives = append(alternatives, part)
			}
		}
		if len(alternatives) < 2 {
			return "", fmt.Errorf("search term %q contains \" OR \" but does not have two alternatives", tag)
		}
		terms = append(terms, alternatives)
	}
	data, err := json.Marshal(terms)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SearchFileIDs implements Client.
func (c *HTTPClient) SearchFileIDs(ctx context.Context, tags []string) ([]int, error) {
	encoded, err := serializeSearchTags(tags)
	if err != nil {
		return nil, err
	}
	var resp struct {
		FileIDs []int `json:"file_ids"`
	}
	path := "get_files/search_files?tags=" + escapeDataString(encoded) + "&return_file_ids=true"
	if err := c.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}
	if resp.FileIDs == nil {
		return []int{}, nil
	}
	return resp.FileIDs, nil
}

// FileMetadata implements Client. An empty id list makes no request.
func (c *HTTPClient) FileMetadata(ctx context.Context, fileIDs []int) ([]FileMetadata, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	ids, err := json.Marshal(fileIDs)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Metadata []FileMetadata `json:"metadata"`
	}
	if err := c.getJSON(ctx, "get_files/file_metadata?file_ids="+escapeDataString(string(ids)), &resp); err != nil {
		return nil, err
	}
	return resp.Metadata, nil
}

// AddTags implements Client. Hydrus applies the same tags to every hash, so
// callers group files by identical tag set. Nothing to send makes no request.
func (c *HTTPClient) AddTags(ctx context.Context, hashes []string, serviceKey string, tags []string) error {
	if len(hashes) == 0 || len(tags) == 0 {
		return nil
	}
	body, err := json.Marshal(struct {
		Hashes            []string            `json:"hashes"`
		ServiceKeysToTags map[string][]string `json:"service_keys_to_tags"`
	}{
		Hashes:            hashes,
		ServiceKeysToTags: map[string][]string{serviceKey: tags},
	})
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "add_tags/add_tags", body)
	return err
}
