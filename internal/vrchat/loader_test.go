package vrchat

import (
	"slices"
	"strings"
	"testing"
)

// Covers the loader's per-file behaviour: the JSON > XML > line priority
// contest, and editor provenance, which is collected independently of it.

const loaderVRCXJSON = `{"application":"VRCX","version":1,
 "author":{"id":"usr_json","displayName":"JsonUser"},
 "world":{"name":"JsonWorld","id":"wrld_json","instanceId":"wrld_json:1"},
 "players":[]}`

const loaderVRCXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>XmpUser</xmp:Author>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
      <vrc:WorldDisplayName>XmpWorld</vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`

// loaderAdobeXMP is Adobe-resaved: no vrc: namespace, VRCX JSON inside
// dc:description.
const loaderAdobeXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:CreatorTool="Adobe Photoshop Express (Android)">
   <dc:description>
    <rdf:Alt>
     <rdf:li xml:lang="x-default">{"application":"VRCX","version":1,"author":{"id":"usr_embedded","displayName":"EmbeddedUser"},"world":{"name":"EmbeddedWorld","id":"wrld_embedded","instanceId":"wrld_embedded:7"},"players":[]}</rdf:li>
    </rdf:Alt>
   </dc:description>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

// loaderNonVRChatXMP is an Adobe packet from an image that was never a
// VRChat screenshot.
const loaderNonVRChatXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:CreatorTool="GIMP 2.10.34"/>
 </rdf:RDF>
</x:xmpmeta>`

const loaderLegacyLine = "screenshotmanager|0|author:usr_line,LineUser|world:wrld_line,99,LineWorld"

const xmpAuthorID = "usr_56e86082-c91c-40a4-bb92-2486ceca90eb"

func description(text, contentType string) Chunk {
	return Chunk{Keyword: KeywordDescription, Text: text, ContentType: contentType}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name   string
		chunks []Chunk
		// wantAuthor "" means Load must return nil.
		wantAuthor string
		wantWorld  string
		wantEditor []string
	}{
		{name: "no chunks", chunks: nil},
		{name: "nothing parses", chunks: []Chunk{description("not metadata at all", ContentTypeText)}},
		{
			// GIMP comments and GameDVR chunks live in the same table.
			name:   "unrelated keyword is ignored",
			chunks: []Chunk{{Keyword: "Comment", Text: loaderVRCXJSON, ContentType: ContentTypeJSON}},
		},
		{
			name:       "VRCX JSON",
			chunks:     []Chunk{description(loaderVRCXJSON, ContentTypeJSON)},
			wantAuthor: "usr_json", wantWorld: "JsonWorld",
		},
		{
			name:       "XMP",
			chunks:     []Chunk{description(loaderVRCXMP, ContentTypeXML)},
			wantAuthor: xmpAuthorID, wantWorld: "XmpWorld", wantEditor: []string{"VRChat"},
		},
		{
			name:       "legacy line",
			chunks:     []Chunk{description(loaderLegacyLine, ContentTypeLine)},
			wantAuthor: "usr_line", wantWorld: "LineWorld",
		},
		{
			name:       "JSON beats XMP, JSON first",
			chunks:     []Chunk{description(loaderVRCXJSON, ContentTypeJSON), description(loaderVRCXMP, ContentTypeXML)},
			wantAuthor: "usr_json", wantWorld: "JsonWorld", wantEditor: []string{"VRChat"},
		},
		{
			name:       "JSON beats XMP, XMP first",
			chunks:     []Chunk{description(loaderVRCXMP, ContentTypeXML), description(loaderVRCXJSON, ContentTypeJSON)},
			wantAuthor: "usr_json", wantWorld: "JsonWorld", wantEditor: []string{"VRChat"},
		},
		{
			name:       "XMP beats the legacy line",
			chunks:     []Chunk{description(loaderLegacyLine, ContentTypeLine), description(loaderVRCXMP, ContentTypeXML)},
			wantAuthor: xmpAuthorID, wantWorld: "XmpWorld", wantEditor: []string{"VRChat"},
		},
		{
			name: "first chunk wins among equal priority",
			chunks: []Chunk{
				description(loaderVRCXJSON, ContentTypeJSON),
				description(strings.ReplaceAll(loaderVRCXJSON, "usr_json", "usr_second"), ContentTypeJSON),
			},
			wantAuthor: "usr_json", wantWorld: "JsonWorld",
		},
		{
			// The recovered payload is a full VRCX record, so it outranks a
			// real XMP packet even though it arrived inside one.
			name:       "embedded JSON is promoted to JSON priority",
			chunks:     []Chunk{description(loaderVRCXMP, ContentTypeXML), description(loaderAdobeXMP, ContentTypeXML)},
			wantAuthor: "usr_embedded", wantWorld: "EmbeddedWorld",
			wantEditor: []string{"VRChat", "Adobe Photoshop Express (Android)"},
		},
		{
			// The point of tracking editor software separately: the JSON chunk
			// wins the metadata contest, but the app that edited the image is
			// only recorded in the XMP chunk.
			name:       "editor provenance survives losing the contest",
			chunks:     []Chunk{description(loaderVRCXJSON, ContentTypeJSON), description(loaderAdobeXMP, ContentTypeXML)},
			wantAuthor: "usr_json", wantWorld: "JsonWorld",
			wantEditor: []string{"Adobe Photoshop Express (Android)"},
		},
		{
			name:       "editor provenance is de-duplicated across chunks",
			chunks:     []Chunk{description(loaderAdobeXMP, ContentTypeXML), description(loaderAdobeXMP, ContentTypeXML)},
			wantAuthor: "usr_embedded", wantWorld: "EmbeddedWorld",
			wantEditor: []string{"Adobe Photoshop Express (Android)"},
		},
		{
			// Three quarters of the cached chunks look like this: valid XMP, no
			// vrc: namespace, no embedded VRCX. The line parser then rejects
			// it, and (matching the Python) its CreatorTool is lost with it.
			name:   "non-VRChat Adobe packet yields nothing",
			chunks: []Chunk{description(loaderNonVRChatXMP, ContentTypeXML)},
		},
		{
			name:       "missing content type falls back to the XML heuristic",
			chunks:     []Chunk{description(loaderVRCXMP, "")},
			wantAuthor: xmpAuthorID, wantWorld: "XmpWorld", wantEditor: []string{"VRChat"},
		},
		{
			// Migration 004 reclassified these, but a chunk stored as 'text'
			// that is really line format must still parse.
			name:       "unrecognized content type is tried as line",
			chunks:     []Chunk{description(loaderLegacyLine, ContentTypeText)},
			wantAuthor: "usr_line", wantWorld: "LineWorld",
		},
		{
			name: "a broken chunk does not stop a good one",
			chunks: []Chunk{
				description("{ this is not valid json", ContentTypeJSON),
				description(loaderVRCXMP, ContentTypeXML),
			},
			wantAuthor: xmpAuthorID, wantWorld: "XmpWorld", wantEditor: []string{"VRChat"},
		},
		{
			name:       "the Adobe XMP keyword is read as well as Description",
			chunks:     []Chunk{{Keyword: KeywordAdobeXMP, Text: loaderVRCXMP, ContentType: ContentTypeXML}},
			wantAuthor: xmpAuthorID, wantWorld: "XmpWorld", wantEditor: []string{"VRChat"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := Load(tt.chunks)
			if tt.wantAuthor == "" {
				if meta != nil {
					t.Fatalf("got %+v, want nil", meta)
				}
				return
			}
			if meta == nil {
				t.Fatal("got nil")
			}
			if meta.Author.ID != tt.wantAuthor || meta.World.Name != tt.wantWorld {
				t.Errorf("author/world = %q/%q, want %q/%q",
					meta.Author.ID, meta.World.Name, tt.wantAuthor, tt.wantWorld)
			}
			if !slices.Equal(meta.EditorSoftware, tt.wantEditor) {
				t.Errorf("editor = %q, want %q", meta.EditorSoftware, tt.wantEditor)
			}
		})
	}
}
