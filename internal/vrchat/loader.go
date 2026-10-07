package vrchat

import (
	"errors"
	"slices"
	"strings"
)

// Chunk is one cached iTXt chunk, as the loader needs it.
//
// The stored columns are nullable, but the loader treats null exactly like
// empty in each of them (a null keyword matches no VRChat keyword, a null text
// sanitizes to "", a null content type falls back like ""), so plain strings
// lose nothing here. Callers that report the stored values (such as the parity
// dump) must keep the distinction themselves.
type Chunk struct {
	Keyword     string
	Text        string
	ContentType string
}

// priority ranks the content types: a richer VRCX JSON payload beats an XMP
// packet, which beats the legacy pipe-delimited line.
func priority(contentType string) int {
	switch contentType {
	case ContentTypeJSON:
		return 3
	case ContentTypeXML:
		return 2
	}
	return 1
}

// Load resolves one file's cached iTXt chunks into a single normalized
// metadata record, or nil if no chunk yielded any. The original Python ran
// this over the whole table at once; the priority contest was always per file,
// so it is done one file at a time here.
func Load(chunks []Chunk) *Metadata {
	var best *Metadata
	bestType := ""

	// Editor provenance is orthogonal to the priority contest: a file's VRCX
	// JSON may win on priority while the editor software lives in a separate
	// XMP chunk. Collect it independently and merge at the end.
	var editorSoftware []string

	for _, chunk := range chunks {
		if chunk.Keyword != KeywordDescription && chunk.Keyword != KeywordAdobeXMP {
			continue
		}

		rawText := SanitizeITXt(chunk.Text)
		contentType := StoredContentType(chunk.ContentType, rawText)

		meta, editor, effective, err := ParseChunk(rawText, contentType)
		if err != nil {
			// Irreparably broken chunk. Another chunk on the same file may
			// still parse, so this is a continue rather than a failure.
			continue
		}

		// Recorded even when the chunk loses the priority contest; that is the
		// point of tracking it separately.
		for _, s := range editor {
			if !slices.Contains(editorSoftware, s) {
				editorSoftware = append(editorSoftware, s)
			}
		}

		if best != nil && priority(effective) <= priority(bestType) {
			continue
		}
		best = meta
		bestType = effective
	}

	if best != nil && len(editorSoftware) > 0 {
		best.EditorSoftware = editorSoftware
	}
	return best
}

// StoredContentType resolves a chunk's stored content type to the parser to
// try: json and xml as stored, an empty type sniffed for XMP, and anything
// else (line, text, unknown) attempted as the legacy line format.
func StoredContentType(stored, rawText string) string {
	ct := strings.ToLower(stored)
	if ct == "" && IsXMPXML(rawText) {
		ct = ContentTypeXML
	}
	if ct != ContentTypeJSON && ct != ContentTypeXML {
		ct = ContentTypeLine
	}
	return ct
}

// ParseChunk parses one sanitized chunk with the parser for contentType (one
// of ContentTypeJSON, ContentTypeXML or ContentTypeLine; anything else is
// treated as line).
//
// It returns the metadata, the editor software named by an XMP packet, and the
// content type the chunk turned out to be: an XMP packet can resolve to JSON
// (Adobe-resaved, VRCX payload in dc:description) or to line, and that
// changes its priority.
//
// The error is a *JSONError, *XMPParseError or *MetaParseError. An XMP packet
// that is neither VRChat XMP nor embedded VRCX falls through to the line
// parser, so the error reported for it is the line parser's, matching the
// original. Its editor software is then lost with it.
func ParseChunk(rawText, contentType string) (meta *Metadata, editorSoftware []string, effectiveType string, err error) {
	switch contentType {
	case ContentTypeJSON:
		root, err := parseJSON(rawText)
		if err != nil {
			return nil, nil, "", err
		}
		return NormalizeJSON(root, rawText), []string{}, ContentTypeJSON, nil

	case ContentTypeXML:
		effective := ContentTypeXML
		xmp, err := ParseXMP(rawText)
		if err == nil {
			meta = NormalizeXMP(xmp, rawText)
		} else {
			var xerr *XMPParseError
			if !errors.As(err, &xerr) {
				return nil, nil, "", err
			}
			// Adobe-edited VRChat screenshots wrap the original VRCX JSON
			// inside dc:description; recover it if present.
			if embedded, ok := ExtractEmbeddedVRCXJSON(rawText); ok {
				meta = NormalizeJSON(embedded, rawText)
				effective = ContentTypeJSON
			} else {
				meta, err = ParseMetaLine(rawText)
				if err != nil {
					return nil, nil, "", err
				}
				effective = ContentTypeLine
			}
		}
		return meta, ExtractEditorSoftware(rawText), effective, nil
	}

	meta, err = ParseMetaLine(rawText)
	if err != nil {
		return nil, nil, "", err
	}
	return meta, []string{}, ContentTypeLine, nil
}
