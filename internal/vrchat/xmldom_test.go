package vrchat

import (
	"errors"
	"strings"
	"testing"
)

// These cover the strictness loadXML layers over encoding/xml so that it
// accepts and rejects what the original tools' parsers (.NET's XmlReader and
// Python's ElementTree) did.

func TestLoadXMLRejects(t *testing.T) {
	tests := map[string]string{
		// encoding/xml leaves an undeclared prefix in Name.Space; ElementTree
		// and XmlReader both fail with "unbound prefix".
		"unbound element prefix":   `<a:root/>`,
		"unbound attribute prefix": `<root a:b="1"/>`,
		"DOCTYPE":                  `<!DOCTYPE root><root/>`,
		"multiple roots":           `<a/><b/>`,
		"text after root":          `<a/>text`,
		"no root":                  `<?xml version="1.0"?>`,
		"mismatched end tag":       `<a></b>`,
		"same local, other prefix": `<x:a xmlns:x="u" xmlns:y="u"></y:a>`,
		"unclosed":                 `<a><b></b>`,
		"duplicate attribute":      `<a b="1" b="2"/>`,
		"duplicate expanded attr":  `<a xmlns:p="u" xmlns:q="u" p:b="1" q:b="2"/>`,
		"double hyphen in comment": `<a><!-- x -- y --></a>`,
		"cdata end in text":        `<a>]]></a>`,
		"bad character reference":  `<a>&#1;</a>`,
		"undefined entity":         `<a>&nbsp;</a>`,
		"late XML declaration":     ` <?xml version="1.0"?><a/>`,
		"empty namespace binding":  `<p:a xmlns:p=""/>`,
	}
	for name, xml := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadXML(xml); !errors.Is(err, errXML) {
				t.Errorf("loadXML(%q) error = %v, want an XML error", xml, err)
			}
		})
	}
}

func TestLoadXMLAccepts(t *testing.T) {
	root, err := loadXML(`<?xml version="1.0" encoding="UTF-16"?>
<!-- leading comment -->
<x:a xmlns:x="urn:x" xmlns="urn:default" plain="1" xml:lang="en"><b>t<![CDATA[<c>]]></b></x:a>
<?xpacket end="w"?>
`)
	if err != nil {
		t.Fatal(err)
	}
	if root.Name != (xmlName{Space: "urn:x", Local: "a"}) {
		t.Errorf("root = %+v", root.Name)
	}
	b := root.elements()[0]
	if b.Name != (xmlName{Space: "urn:default", Local: "b"}) {
		t.Errorf("child = %+v", b.Name)
	}
	if txt, _ := b.directText(); txt != "t<c>" {
		t.Errorf("text = %q", txt)
	}

	// Unprefixed attributes are in no namespace; namespace declarations are
	// attributes too, as in XDocument.
	want := []xmlAttr{
		{xmlName{xmlnsNamespaceURI, "x"}, "urn:x"},
		{xmlName{"", "xmlns"}, "urn:default"},
		{xmlName{"", "plain"}, "1"},
		{xmlName{xmlNamespaceURI, "lang"}, "en"},
	}
	if len(root.Attrs) != len(want) {
		t.Fatalf("attrs = %+v", root.Attrs)
	}
	for i := range want {
		if root.Attrs[i] != want[i] {
			t.Errorf("attr %d = %+v, want %+v", i, root.Attrs[i], want[i])
		}
	}
}

func TestLoadXMLDirectTextStopsAtACommentOrChild(t *testing.T) {
	root, err := loadXML(`<a>one<!-- c -->two<b/>three</a>`)
	if err != nil {
		t.Fatal(err)
	}
	if txt, ok := root.directText(); txt != "one" || !ok {
		t.Errorf("directText = %q, %v", txt, ok)
	}

	root, err = loadXML(`<a><b/>tail</a>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := root.directText(); ok {
		t.Error("an element whose first child is an element has no direct text")
	}
}

func TestLoadXMLNormalizesAttributeValues(t *testing.T) {
	// Literal whitespace becomes a space; a character reference survives.
	root, err := loadXML("<a v=\"x\r\ny\tz&#xA;w&#9;&amp;\"/>")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := root.Attrs[0].Value, "x y z\nw\t&"; got != want {
		t.Errorf("value = %q, want %q", got, want)
	}

	// Text content keeps its newlines, CRLF unified to LF.
	root, err = loadXML("<a>x\r\ny</a>")
	if err != nil {
		t.Fatal(err)
	}
	if txt, _ := root.directText(); txt != "x\ny" {
		t.Errorf("text = %q", txt)
	}
}

func TestLoadXMLCapsNestingDepth(t *testing.T) {
	nested := func(depth int) string {
		return strings.Repeat("<a>", depth) + strings.Repeat("</a>", depth)
	}

	if _, err := loadXML(nested(maxXMLDepth)); err != nil {
		t.Errorf("depth %d: %v, want accepted", maxXMLDepth, err)
	}
	if _, err := loadXML(nested(maxXMLDepth + 1)); !errors.Is(err, errXML) {
		t.Errorf("depth %d: error = %v, want an XML error", maxXMLDepth+1, err)
	}
	// Far deeper than the cap: rejected promptly, not walked.
	if _, err := loadXML(nested(1_000_000)); !errors.Is(err, errXML) {
		t.Errorf("depth 1e6: error = %v, want an XML error", err)
	}
}
