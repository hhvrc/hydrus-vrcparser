package vrchat

import "strings"

// Content types classify an iTXt chunk into the formats this package knows,
// stored in the itxt_chunks.content_type column.
const (
	ContentTypeJSON = "json"
	ContentTypeXML  = "xml"
	ContentTypeLine = "line"

	// ContentTypeText is unrecognized or non-VRChat content (GIMP comments,
	// GameDVR, ...).
	ContentTypeText = "text"
)

// iTXt keywords that can carry VRChat metadata.
const (
	KeywordDescription = "Description"
	KeywordAdobeXMP    = "XML:com.adobe.xmp"
)

// DetectContentType classifies a chunk's text, returning false when nothing
// recognizes it. Same rules as the original Python's format detection.
//
// Order matters and matches the original: the XMP keyword short-circuits to
// xml regardless of content, then JSON, then XMP-shaped XML, then the legacy
// line format.
//
// A NULL text and an empty one are rejected alike by every check, so a plain
// string loses nothing.
func DetectContentType(text, keyword string) (string, bool) {
	if keyword == KeywordAdobeXMP {
		return ContentTypeXML, true
	}
	if IsJSON(text) {
		return ContentTypeJSON, true
	}
	if IsXMPXML(text) {
		return ContentTypeXML, true
	}
	if _, err := ParseMetaLine(text); err == nil {
		return ContentTypeLine, true
	}
	return "", false
}

// IsJSON reports whether text is any valid JSON document, not just an object,
// matching Python's json.loads, which accepts bare arrays and scalars too.
func IsJSON(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	_, err := parseJSON(text)
	return err == nil
}

// IsXMPXML reports whether text parses as XML rooted at xmpmeta or a bare
// rdf:RDF. A namespace is required: the original compared against
// "}xmpmeta", which a namespace-less root can never match.
func IsXMPXML(text string) bool {
	if !strings.HasPrefix(strings.TrimSpace(text), "<") {
		return false
	}
	root, err := loadXML(text)
	if err != nil || root.Name.Space == "" {
		return false
	}
	return root.Name.Local == "xmpmeta" || root.Name.Local == "RDF"
}
