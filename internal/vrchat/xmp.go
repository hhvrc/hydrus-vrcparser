package vrchat

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// XMPParseError reports an XMP packet that is structurally invalid or carries
// no usable VRChat identifier. The parity dump records it as Python's
// "XMPParseError".
type XMPParseError struct {
	Msg string
	Err error // underlying XML error, if any
}

func (e *XMPParseError) Error() string { return e.Msg }

func (e *XMPParseError) Unwrap() error { return e.Err }

// XMPMetadata is the typed result of a native VRChat XMP packet.
type XMPMetadata struct {
	RawXML            string
	CreatorTool       *string
	AuthorID          *string
	AuthorDisplayName *string
	Created           *time.Time
	Modified          *time.Time
	TiffDateTime      *time.Time
	WorldID           *string
	WorldName         *string
}

const (
	nsX    = "adobe:ns:meta/"
	nsRDF  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsXMP  = "http://ns.adobe.com/xap/1.0/"
	nsTIFF = "http://ns.adobe.com/tiff/1.0/"
	nsVRC  = "http://ns.vrchat.com/vrc/1.0/"
)

var (
	userIDPattern  = regexp.MustCompile(`^usr_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	worldIDPattern = regexp.MustCompile(`^wrld_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// ParseXMP parses a VRChat XMP packet.
//
// Two forms exist. In the normal form the VRC namespace carries WorldID /
// WorldDisplayName / AuthorID, and xmp:Author is a human-readable name. In the
// compact form only vrc:World is present, and xmp:Author may itself be a usr_
// id. Author is only ever interpreted as an id when the compact form is
// detected.
//
// A packet that is structurally invalid, or carries no usable VRChat
// identifier, yields a *XMPParseError.
func ParseXMP(xmlText string) (*XMPMetadata, error) {
	grouped, err := parseXMPOuter(xmlText)
	if err != nil {
		return nil, err
	}

	// textOf is the trimmed direct text of the element, or nil when the
	// element is absent. Present-but-empty is "", which matters for vrc:World.
	textOf := func(ns, local string) *string {
		e, ok := grouped[ns][local]
		if !ok {
			return nil
		}
		t, _ := e.directText()
		return strPtr(strings.TrimSpace(t))
	}
	nonEmpty := func(s *string) bool { return s != nil && *s != "" }

	creatorTool := textOf(nsXMP, "CreatorTool")
	rawAuthor := textOf(nsXMP, "Author")

	var worldID, worldName, authorID, authorName *string

	if wid := textOf(nsVRC, "WorldID"); nonEmpty(wid) && worldIDPattern.MatchString(*wid) {
		worldID = wid
	}
	if wname := textOf(nsVRC, "WorldDisplayName"); nonEmpty(wname) {
		worldName = wname
	}
	if aid := textOf(nsVRC, "AuthorID"); nonEmpty(aid) && userIDPattern.MatchString(*aid) {
		authorID = aid
	}

	// Presence, not truthiness: an empty <vrc:World/> still marks the compact
	// form.
	worldCompact := textOf(nsVRC, "World")
	if worldCompact != nil {
		if worldIDPattern.MatchString(*worldCompact) {
			worldID = worldCompact
		}
		if nonEmpty(rawAuthor) && userIDPattern.MatchString(*rawAuthor) {
			// An explicit vrc:AuthorID wins over an inferred one.
			if authorID == nil {
				authorID = rawAuthor
			}
			authorName = nil
		} else {
			authorName = rawAuthor
		}
	} else {
		authorName = rawAuthor
	}

	if worldID == nil && authorID == nil {
		return nil, &XMPParseError{Msg: "Missing or invalid VRChat identifiers in XMP metadata"}
	}

	return &XMPMetadata{
		RawXML:            xmlText,
		CreatorTool:       creatorTool,
		AuthorID:          authorID,
		AuthorDisplayName: authorName,
		Created:           parseXMPDateTime(textOf(nsXMP, "CreateDate")),
		Modified:          parseXMPDateTime(textOf(nsXMP, "ModifyDate")),
		TiffDateTime:      parseXMPDateTime(textOf(nsTIFF, "DateTime")),
		WorldID:           worldID,
		WorldName:         worldName,
	}, nil
}

// parseXMPOuter validates the envelope and groups rdf:Description children by
// namespace and local name.
func parseXMPOuter(xmlText string) (map[string]map[string]*xmlElement, error) {
	root, err := loadXML(xmlText)
	if err != nil {
		return nil, &XMPParseError{Msg: "XML parsing failed: " + err.Error(), Err: err}
	}

	if root.Name != (xmlName{Space: nsX, Local: "xmpmeta"}) {
		return nil, &XMPParseError{Msg: "Invalid root element: expected {" + nsX + "}xmpmeta"}
	}

	var rdf []*xmlElement
	for _, c := range root.elements() {
		if c.Name == (xmlName{Space: nsRDF, Local: "RDF"}) {
			rdf = append(rdf, c)
		}
	}
	if len(rdf) != 1 {
		return nil, &XMPParseError{Msg: "Expected exactly one rdf:RDF child element"}
	}

	grouped := map[string]map[string]*xmlElement{}
	for _, desc := range rdf[0].elements() {
		if desc.Name != (xmlName{Space: nsRDF, Local: "Description"}) {
			continue
		}
		for _, elem := range desc.elements() {
			ns := elem.Name.Space
			if ns == "" {
				// Elements without a namespace are not addressable here.
				continue
			}
			bucket := grouped[ns]
			if bucket == nil {
				bucket = map[string]*xmlElement{}
				grouped[ns] = bucket
			}
			if _, dup := bucket[elem.Name.Local]; dup {
				return nil, &XMPParseError{Msg: fmt.Sprintf(
					"Duplicate element for {%s}%s in XMP metadata", ns, elem.Name.Local)}
			}
			bucket[elem.Name.Local] = elem
		}
	}
	return grouped, nil
}

// ExtractEmbeddedVRCXJSON recovers VRCX JSON embedded in an XMP packet.
//
// VRChat screenshots re-saved by Adobe apps lose the vrc: namespace but keep
// the original VRCX JSON inside dc:description, typically as
// <dc:description><rdf:Alt><rdf:li>{...}</rdf:li>. ParseXMP rejects those
// packets, so this recovers the payload for the standard JSON path. It
// returns the decoded JSON object (see NormalizeJSON for its shape), or false
// when nothing is embedded or the XML does not parse.
func ExtractEmbeddedVRCXJSON(xmlText string) (map[string]any, bool) {
	root, err := loadXML(xmlText)
	if err != nil {
		return nil, false
	}

	var found map[string]any
	root.descendantsAndSelf(func(e *xmlElement) {
		if found != nil {
			return
		}
		t, _ := e.directText()
		txt := strings.TrimSpace(t)

		// Cheap pre-filter before attempting a decode on every text node.
		if !strings.HasPrefix(txt, "{") {
			return
		}
		if !strings.Contains(txt, `"world"`) && !strings.Contains(txt, `"author"`) {
			return
		}

		v, err := parseJSON(txt)
		if err != nil {
			return
		}
		if obj, ok := v.(map[string]any); ok && (jsonTruthy(obj, "world") || jsonTruthy(obj, "author")) {
			found = obj
		}
	})
	return found, found != nil
}

// ExtractEditorSoftware names the software that created or edited the image:
// xmp:CreatorTool plus every stEvt:softwareAgent in the xmpMM:History log.
// Both appear as elements (native VRChat XMP) or attributes (Adobe's compact
// RDF), so they are matched by local name across both. The result is
// order-preserving and de-duplicated; invalid XML yields none.
func ExtractEditorSoftware(xmlText string) []string {
	found := []string{}

	root, err := loadXML(xmlText)
	if err != nil {
		return found
	}

	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(found, v) {
			found = append(found, v)
		}
	}

	root.descendantsAndSelf(func(e *xmlElement) {
		if e.Name.Local == "CreatorTool" || e.Name.Local == "softwareAgent" {
			t, _ := e.directText()
			add(t)
		}
		for _, a := range e.Attrs {
			if a.Name.Local == "CreatorTool" || a.Name.Local == "softwareAgent" {
				add(a.Value)
			}
		}
	})
	return found
}
