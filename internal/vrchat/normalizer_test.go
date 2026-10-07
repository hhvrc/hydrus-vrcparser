package vrchat

import (
	"testing"
	"time"
)

// Cases carried over from the original Python normalizer and
// format-detection tests.

func normalizeJSONText(t *testing.T, text string) *Metadata {
	t.Helper()
	root, err := parseJSON(text)
	if err != nil {
		t.Fatalf("parseJSON(%q): %v", text, err)
	}
	return NormalizeJSON(root, "raw")
}

func TestNormalizeJSON(t *testing.T) {
	tests := []struct {
		name  string
		json  string
		check func(t *testing.T, m *Metadata)
	}{
		{
			name: "typical payload",
			json: `{"author": {"id": "usr_abc", "displayName": "User"}, "world": {"id": "wrld_xyz", "name": "World"}}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author != (Author{"usr_abc", "User"}) || m.World.ID != "wrld_xyz" ||
					m.World.Name != "World" || m.RawText != "raw" {
					t.Errorf("got %+v", m)
				}
			},
		},
		{
			name: "every missing field defaults",
			json: `{}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author != (Author{}) || m.World != (World{}) || m.Position != (Position{}) ||
					m.Rq != 0 || len(m.Players) != 0 || m.Created != nil || len(m.EditorSoftware) != 0 {
					t.Errorf("got %+v", m)
				}
			},
		},
		{
			name: "displayName falls back to name",
			json: `{"author": {"id": "usr_abc", "name": "FallbackName"}}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author.DisplayName != "FallbackName" {
					t.Errorf("display name = %q", m.Author.DisplayName)
				}
			},
		},
		{
			name: "numeric strings in position are coerced",
			json: `{"position": {"x": "1.5", "y": 2.0, "z": "3.5"}}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Position != (Position{1.5, 2, 3.5}) {
					t.Errorf("position = %+v", m.Position)
				}
			},
		},
		{
			name: "good coordinates survive an unparseable one",
			json: `{"position": {"x": "not_a_number", "y": 1.0, "z": 2.0}}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Position != (Position{0, 1, 2}) {
					t.Errorf("position = %+v", m.Position)
				}
			},
		},
		{
			name: "null author is absent",
			json: `{"author": null}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author != (Author{}) {
					t.Errorf("author = %+v", m.Author)
				}
			},
		},
		{
			name: "players",
			json: `{"players": [{"id": "usr_p1", "displayName": "P1"}, {"id": "usr_p2", "displayName": "P2"}]}`,
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 2 || m.Players[0].ID != "usr_p1" || m.Players[1].DisplayName != "P2" {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
		{
			// The Python passed the list straight through, so a bare string
			// here later raised an AttributeError it did not catch. Skipping is
			// safer and cannot change the result for well-formed data.
			name: "non-object player entries are skipped",
			json: `{"players": ["not-an-object", {"id": "usr_p1"}]}`,
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 1 || m.Players[0].ID != "usr_p1" {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
		{
			// Structure taken from a content_type='json' row in vrchat.db.
			name: "real VRCX shape",
			json: `{
  "application": "VRCX",
  "version": 1,
  "author": {"id": "usr_c348aa66-98e5-4f64-96f3-62e7a14187d1", "displayName": "ComfyHeaven"},
  "world": {
    "name": "Sunset Bash",
    "id": "wrld_f96f9d27-fb3b-4f68-b9d5-94b4d431aab2",
    "instanceId": "wrld_f96f9d27-fb3b-4f68-b9d5-94b4d431aab2:81786~group(grp_d1)~region(us)"
  },
  "players": [{"id": "usr_db6e5c3a-c84f-4a4f-9929-061f3b73ced8", "displayName": "Nitrosaki"}]
}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author.DisplayName != "ComfyHeaven" || m.World.Name != "Sunset Bash" ||
					m.World.InstanceID[:13] != "wrld_f96f9d27" || len(m.Players) != 1 {
					t.Errorf("got %+v", m)
				}
				// VRCX payloads carry no type/index/position/rq.
				if m.Type != nil || m.Index != nil || m.Rq != 0 {
					t.Errorf("type/index/rq = %v/%v/%d", m.Type, m.Index, m.Rq)
				}
			},
		},
		{
			// Real rows contain double-encoded UTF-8; the tag must match byte
			// for byte.
			name: "mis-encoded display names are preserved",
			json: "{\"players\": [{\"id\": \"usr_x\", \"displayName\": \"Night\xc3\xa2\xcb\x86\xe2\x80\x94\"}]}",
			check: func(t *testing.T, m *Metadata) {
				if m.Players[0].DisplayName != "Night\xc3\xa2\xcb\x86\xe2\x80\x94" {
					t.Errorf("display name = %q", m.Players[0].DisplayName)
				}
			},
		},
		{
			// json.loads accepts bare arrays and scalars; normalizing one must
			// not fail.
			name: "non-object root",
			json: `[1,2,3]`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author.ID != "" || len(m.Players) != 0 {
					t.Errorf("got %+v", m)
				}
			},
		},
		{
			name: "type, index and creator_tool",
			json: `{"type": "", "index": 3, "creator_tool": "Tool"}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Type == nil || *m.Type != "" || m.Index == nil || *m.Index != 3 || deref(m.CreatorTool) != "Tool" {
					t.Errorf("type/index/creator = %v/%v/%v", m.Type, m.Index, m.CreatorTool)
				}
			},
		},
		{
			// TryGetInt32 takes integer literals only.
			name: "fractional index is absent",
			json: `{"index": 3.0}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Index != nil {
					t.Errorf("index = %d", *m.Index)
				}
			},
		},
		{
			// Duplicate keys: the last wins, as in Python's dict and
			// JsonElement.TryGetProperty.
			name: "duplicate keys",
			json: `{"author": {"id": "first"}, "author": {"id": "second"}}`,
			check: func(t *testing.T, m *Metadata) {
				if m.Author.ID != "second" {
					t.Errorf("author id = %q", m.Author.ID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, normalizeJSONText(t, tt.json))
		})
	}
}

func TestNormalizeJSONRenderQualityCoercesLikePythonInt(t *testing.T) {
	tests := map[string]int{
		`{"rq": 4}`:     4,
		`{"rq": "5"}`:   5,
		`{"rq": 5.7}`:   5,
		`{"rq": "5.7"}`: 0,
		`{"rq": null}`:  0,
	}
	for in, want := range tests {
		if got := normalizeJSONText(t, in).Rq; got != want {
			t.Errorf("%s: rq = %d, want %d", in, got, want)
		}
	}
}

func TestNormalizeXMP(t *testing.T) {
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NormalizeXMP(&XMPMetadata{
		CreatorTool:       strPtr("VRChat"),
		AuthorID:          strPtr("usr_abc"),
		AuthorDisplayName: strPtr("User"),
		WorldID:           strPtr("wrld_xyz"),
		WorldName:         strPtr("World"),
		Created:           &created,
	}, "raw")

	if deref(m.Type) != "xmp" || deref(m.CreatorTool) != "VRChat" || m.Author.ID != "usr_abc" ||
		m.World.ID != "wrld_xyz" || !m.Created.Equal(created) {
		t.Errorf("got %+v", m)
	}
	// XMP carries no instance id.
	if m.World.InstanceID != "" {
		t.Errorf("instance = %q", m.World.InstanceID)
	}

	// Absent fields map to empty strings.
	m = NormalizeXMP(&XMPMetadata{WorldID: strPtr("wrld_xyz")}, "raw")
	if m.Author != (Author{}) || m.World.Name != "" {
		t.Errorf("got %+v", m)
	}
}

func TestDetectContentType(t *testing.T) {
	const xmp = `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>`
	tests := []struct {
		name, text, keyword, want string
		wantOK                    bool
	}{
		{"json object", `{"key": "value"}`, "", ContentTypeJSON, true},
		{"json array", `[1, 2, 3]`, "", ContentTypeJSON, true},
		{"XMP keyword regardless of content", "anything", KeywordAdobeXMP, ContentTypeXML, true},
		{"XMP-shaped XML", xmp, "", ContentTypeXML, true},
		{"legacy line", "screenshotmanager|0|author:usr_abc,TestUser", "", ContentTypeLine, true},
		{"unrecognized", "random garbage", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectContentType(tt.text, tt.keyword)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestIsXMPXML(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>`, true},
		{`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>`, true},
		{`<root><child/></root>`, false},
		{`not xml`, false},
		{``, false},
		{`{"key": "value"}`, false},
		// The original matched against "}xmpmeta", which a namespace-less root
		// can never satisfy.
		{`<xmpmeta/>`, false},
	}
	for _, tt := range tests {
		if got := IsXMPXML(tt.text); got != tt.want {
			t.Errorf("IsXMPXML(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestIsJSONMatchesSystemTextJSONStrictness(t *testing.T) {
	tests := map[string]bool{
		`{"a":1}`:            true,
		`"scalar"`:           true,
		`  `:                 false,
		`{"a":1,}`:           false, // trailing comma
		`NaN`:                false, // Python accepted it; System.Text.Json did not
		`{"a":1} x`:          false,
		`// c` + "\n" + `{}`: false, // comments
	}
	for in, want := range tests {
		if got := IsJSON(in); got != want {
			t.Errorf("IsJSON(%q) = %v, want %v", in, got, want)
		}
	}

	deep := ""
	for range 64 {
		deep = "[" + deep + "]"
	}
	if !IsJSON(deep) {
		t.Error("64 levels of nesting should be accepted")
	}
	if IsJSON("[" + deep + "]") {
		t.Error("65 levels of nesting should be rejected, as JsonDocument's MaxDepth does")
	}
}
