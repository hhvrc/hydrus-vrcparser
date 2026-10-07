package vrchat

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// Cases carried over from the original Python XMP-parser tests.

const normalXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>TestUser</xmp:Author>
      <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
      <xmp:ModifyDate>2025-08-30T06:45:33+02:00</xmp:ModifyDate>
    </rdf:Description>
    <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
      <tiff:DateTime>2025-08-30T06:45:33+02:00</tiff:DateTime>
    </rdf:Description>
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
      <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`

const compactXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</xmp:Author>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:World>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:World>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`

const emptyWorldXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>SomeUser</xmp:Author>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID />
      <vrc:WorldDisplayName></vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`

const nonVRChatXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 5.5.0">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:ModifyDate="2025-07-22T17:39:09+02:00">
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>`

const resavedPhotoViewerXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>Microsoft Windows Photo Viewer 10.0.26100.1882</xmp:CreatorTool>
      <xmp:Author>TestUser</xmp:Author>
      <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
      <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`

// embeddedVRCXXMP is an Adobe-resaved screenshot: the vrc: namespace is gone
// entirely and the original VRCX JSON survives only inside dc:description.
const embeddedVRCXXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 4.4.0-Exiv2">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:CreatorTool="Adobe Photoshop Express (Android)">
   <dc:description>
    <rdf:Alt>
     <rdf:li xml:lang="x-default">{"application":"VRCX","version":1,"author":{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},"world":{"name":"Wild Flower","id":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a","instanceId":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a:60622~private"},"players":[{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},{"id":"usr_c348aa66-98e5-4f64-96f3-62e7a14187d1","displayName":"ComfyHeaven"}]}</rdf:li>
    </rdf:Alt>
   </dc:description>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

func mustParseXMP(t *testing.T, xml string) *XMPMetadata {
	t.Helper()
	r, err := ParseXMP(xml)
	if err != nil {
		t.Fatalf("ParseXMP: %v", err)
	}
	return r
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestParseXMPNormalForm(t *testing.T) {
	r := mustParseXMP(t, normalXMP)

	checks := []struct{ field, got, want string }{
		{"CreatorTool", deref(r.CreatorTool), "VRChat"},
		{"AuthorID", deref(r.AuthorID), "usr_56e86082-c91c-40a4-bb92-2486ceca90eb"},
		{"AuthorDisplayName", deref(r.AuthorDisplayName), "TestUser"},
		{"WorldID", deref(r.WorldID), "wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe"},
		{"WorldName", deref(r.WorldName), "Test World"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestParseXMPPreservesTheOriginalUTCOffset(t *testing.T) {
	r := mustParseXMP(t, normalXMP)

	// The date tag is rendered from this value, and normalizing to UTC could
	// move a late-evening screenshot to the previous day, so the offset must
	// survive, not just the instant.
	want := time.Date(2025, 8, 30, 6, 45, 33, 0, time.FixedZone("", 2*3600))
	if r.Created == nil || !r.Created.Equal(want) {
		t.Fatalf("Created = %v, want %v", r.Created, want)
	}
	if _, off := r.Created.Zone(); off != 2*3600 || r.Created.Day() != 30 {
		t.Errorf("Created offset = %d, day = %d", off, r.Created.Day())
	}
}

func TestParseXMPDateTime(t *testing.T) {
	tests := []struct {
		in         string
		wantNil    bool
		wantHour   int
		wantOffset int
		wantNanos  int
	}{
		{in: "2025-08-30T06:45:33Z", wantHour: 6},
		// Offset-less values are treated as UTC.
		{in: "2025-08-30T06:45:33", wantHour: 6},
		{in: "2025-08-30T06:45:33.1234567-05:00", wantHour: 6, wantOffset: -5 * 3600, wantNanos: 123456700},
		// Fractions round to .NET's 100ns tick.
		{in: "2025-08-30T06:45:33.12345678+00:00", wantHour: 6, wantNanos: 123456800},
		{in: "", wantNil: true},
		{in: "   ", wantNil: true},
		{in: "not a date", wantNil: true},
		{in: "2025-02-30T00:00:00Z", wantNil: true},
		{in: "2025-08-30T24:00:00Z", wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			in := tt.in
			got := parseXMPDateTime(&in)
			if tt.wantNil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil")
			}
			_, off := got.Zone()
			if got.Hour() != tt.wantHour || off != tt.wantOffset || got.Nanosecond() != tt.wantNanos {
				t.Errorf("got %v (offset %d, nanos %d)", got, off, got.Nanosecond())
			}
		})
	}
	if parseXMPDateTime(nil) != nil {
		t.Error("nil input should yield nil")
	}
}

func TestParseXMPCompactForm(t *testing.T) {
	// Author is an id only in the compact form.
	r := mustParseXMP(t, compactXMP)
	if deref(r.AuthorID) != "usr_56e86082-c91c-40a4-bb92-2486ceca90eb" || r.AuthorDisplayName != nil {
		t.Errorf("author = %q / %q", deref(r.AuthorID), deref(r.AuthorDisplayName))
	}
	if deref(r.WorldID) != "wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe" {
		t.Errorf("world = %q", deref(r.WorldID))
	}

	// Compact form, but the author is a display name rather than a usr_ id.
	named := strings.Replace(compactXMP,
		"<xmp:Author>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</xmp:Author>",
		"<xmp:Author>Just A Name</xmp:Author>", 1)
	r = mustParseXMP(t, named)
	if r.AuthorID != nil || deref(r.AuthorDisplayName) != "Just A Name" {
		t.Errorf("author = %q / %q", deref(r.AuthorID), deref(r.AuthorDisplayName))
	}
}

func TestParseXMPIgnoresAnEmptyWorldIDButKeepsTheAuthor(t *testing.T) {
	r := mustParseXMP(t, emptyWorldXMP)
	if deref(r.AuthorID) != "usr_56e86082-c91c-40a4-bb92-2486ceca90eb" ||
		r.WorldID != nil || deref(r.AuthorDisplayName) != "SomeUser" {
		t.Errorf("got %+v", r)
	}
}

func TestParseXMPAcceptsFilesResavedByOtherViewers(t *testing.T) {
	// Windows Photo Viewer overwrites CreatorTool but preserves vrc: data.
	r := mustParseXMP(t, resavedPhotoViewerXMP)
	if deref(r.CreatorTool) != "Microsoft Windows Photo Viewer 10.0.26100.1882" ||
		deref(r.AuthorID) != "usr_56e86082-c91c-40a4-bb92-2486ceca90eb" ||
		deref(r.AuthorDisplayName) != "TestUser" || deref(r.WorldName) != "Test World" {
		t.Errorf("got %+v", r)
	}
}

func TestParseXMPRejections(t *testing.T) {
	tests := []struct {
		name, xml, wantMsg string
	}{
		{name: "no VRChat identifiers", xml: nonVRChatXMP},
		{name: "not xml", xml: "not xml at all"},
		{name: "wrong root", xml: "<root><child/></root>"},
		{name: "neither world nor author id", xml: `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>NoIDs</xmp:Author>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`},
		{name: "duplicate elements in one namespace", wantMsg: "Duplicate", xml: `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
    <rdf:Description>
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
    </rdf:Description>
    <rdf:Description>
      <vrc:WorldID>wrld_00000000-0000-0000-0000-000000000000</vrc:WorldID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`},
		{name: "more than one rdf:RDF", xml: `<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:RDF />
  <rdf:RDF />
</x:xmpmeta>`},
		// No vrc: namespace survives the Adobe round-trip.
		{name: "Adobe-resaved", xml: embeddedVRCXXMP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseXMP(tt.xml)
			var xerr *XMPParseError
			if !errors.As(err, &xerr) {
				t.Fatalf("error = %v, want *XMPParseError", err)
			}
			if !strings.Contains(xerr.Msg, tt.wantMsg) {
				t.Errorf("message %q does not contain %q", xerr.Msg, tt.wantMsg)
			}
		})
	}
}

func TestExtractEmbeddedVRCXJSON(t *testing.T) {
	data, ok := ExtractEmbeddedVRCXJSON(embeddedVRCXXMP)
	if !ok {
		t.Fatal("no embedded JSON found")
	}
	world := data["world"].(map[string]any)
	author := data["author"].(map[string]any)
	if world["id"] != "wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a" ||
		author["id"] != "usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd" ||
		len(data["players"].([]any)) != 2 {
		t.Errorf("got %v", data)
	}

	// Native VRChat data lives in elements, not an embedded JSON blob; and
	// invalid XML yields nothing rather than an error.
	for name, xml := range map[string]string{
		"plain": nonVRChatXMP, "native": normalXMP, "invalid": "not xml at all",
	} {
		if _, ok := ExtractEmbeddedVRCXJSON(xml); ok {
			t.Errorf("%s: found embedded JSON", name)
		}
	}
}

func TestExtractEditorSoftware(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want []string
	}{
		// Adobe's compact RDF form puts CreatorTool on the Description element.
		{"attribute", embeddedVRCXXMP, []string{"Adobe Photoshop Express (Android)"}},
		{"element", normalXMP, []string{"VRChat"}},
		{"edit history", `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
           xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
           xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
    <rdf:Description>
      <xmp:CreatorTool>GIMP 2.10.34</xmp:CreatorTool>
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:action="saved" stEvt:softwareAgent="Adobe Photoshop 25.0"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`, []string{"GIMP 2.10.34", "Adobe Photoshop 25.0"}},
		{"deduplicated in order", `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
           xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
           xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
    <rdf:Description>
      <xmp:CreatorTool>Adobe Photoshop Express (Android)</xmp:CreatorTool>
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:softwareAgent="Adobe Photoshop Express (Android)"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`, []string{"Adobe Photoshop Express (Android)"}},
		{"invalid xml", "not xml", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractEditorSoftware(tt.xml); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
