// Package hydrus is a thin client over the Hydrus client API endpoints this
// tool uses: service lookup, file search, file metadata and add_tags.
package hydrus

import (
	"context"
	"strings"
)

// LocalTagServiceType is the Hydrus service type of a local tag service.
const LocalTagServiceType = 5

// Client is what the tagger host needs from Hydrus. Implemented by *HTTPClient;
// tests substitute a fake.
type Client interface {
	// ResolveLocalTagServiceKey finds a local tag service by name, or, when
	// name is empty, requires exactly one to exist.
	ResolveLocalTagServiceKey(ctx context.Context, name string) (string, error)

	// SearchFileIDs runs a search. Terms are ANDed; a term written "a OR b"
	// becomes a Hydrus OR predicate.
	SearchFileIDs(ctx context.Context, tags []string) ([]int, error)

	// FileMetadata fetches metadata for the given ids in one request. The
	// caller batches.
	FileMetadata(ctx context.Context, fileIDs []int) ([]FileMetadata, error)

	// AddTags applies one tag list to many files in one request.
	AddTags(ctx context.Context, hashes []string, serviceKey string, tags []string) error
}

// Service is one Hydrus service as reported by get_services.
type Service struct {
	Key  string
	Name string
	Type int
}

// FileMetadata is the subset of a file_metadata row this tool reads.
type FileMetadata struct {
	FileID    *int     `json:"file_id"`
	Hash      string   `json:"hash"`
	Ext       string   `json:"ext"`
	KnownURLs []string `json:"known_urls"`

	// Some Hydrus versions report the same data under "urls".
	URLs []string `json:"urls"`

	// Tags is the modern per-service tag report, keyed by service key.
	Tags map[string]ServiceTags `json:"tags"`

	// ServiceKeysToStatusesToTags is the legacy shape of the same data.
	ServiceKeysToStatusesToTags map[string]map[string][]string `json:"service_keys_to_statuses_to_tags"`
}

// ServiceTags is one service's tags on a file, by status ("0" = current).
type ServiceTags struct {
	StorageTags map[string][]string `json:"storage_tags"`
}

// tagStatusCurrent is Hydrus's status key for tags currently on a file.
const tagStatusCurrent = "0"

// CurrentTags returns the file's current storage tags across every tag
// service, so manually added tags count too, from either response shape.
// May contain duplicates when services share a tag.
func (m *FileMetadata) CurrentTags() []string {
	var out []string
	for _, svc := range m.Tags {
		out = append(out, svc.StorageTags[tagStatusCurrent]...)
	}
	for _, statuses := range m.ServiceKeysToStatusesToTags {
		out = append(out, statuses[tagStatusCurrent]...)
	}
	return out
}

// NormalizedExt is the extension without its dot, lowercased, defaulting to
// "png", the format of the screenshots this tool reads.
func (m *FileMetadata) NormalizedExt() string {
	ext := strings.ToLower(strings.TrimLeft(strings.TrimSpace(m.Ext), "."))
	if ext == "" {
		return "png"
	}
	return ext
}

// AllURLs returns every URL recorded against the file, from either field.
func (m *FileMetadata) AllURLs() []string {
	out := make([]string, 0, len(m.KnownURLs)+len(m.URLs))
	out = append(out, m.KnownURLs...)
	return append(out, m.URLs...)
}
