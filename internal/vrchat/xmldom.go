package vrchat

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A minimal XML tree, built from encoding/xml's raw token stream.
//
// The C# loaded every packet through XmlReader with DtdProcessing.Prohibit
// into an XDocument; the Python used xml.etree.ElementTree. encoding/xml is
// considerably more forgiving than either, so this loader layers on the checks
// that decide whether a real packet parses at all:
//
//   - an undeclared namespace prefix is an error ("unbound prefix"), where
//     encoding/xml would leave the prefix in Name.Space;
//   - exactly one root element, and no non-whitespace text outside it;
//   - no DOCTYPE or other markup declaration (the C# prohibited DTDs, which
//     matters since these packets come from arbitrary image files);
//   - start and end tags must match by qualified name (RawToken does not
//     check);
//   - duplicate attributes, "--" inside comments, "]]>" in text, character
//     references to non-XML characters, and an XML declaration anywhere but
//     the very start are errors;
//   - attribute values get XML 1.0 attribute-value normalization (literal
//     tab/newline become a space; a character reference such as &#xA; is
//     kept), which XmlReader and expat both apply and encoding/xml does not.
//
// Whitespace text nodes are kept (the C# set IgnoreWhitespace = false), and
// comments and processing instructions are kept as opaque nodes because they
// end an element's direct text (see directText).

const (
	xmlNamespaceURI   = "http://www.w3.org/XML/1998/namespace"
	xmlnsNamespaceURI = "http://www.w3.org/2000/xmlns/"
)

type xmlName struct {
	Space string // namespace URI, "" when none
	Local string
}

type xmlAttr struct {
	Name  xmlName
	Value string
}

type xmlNodeKind int

const (
	xmlTextNode  xmlNodeKind = iota // text or CDATA
	xmlElemNode                     // child element
	xmlOtherNode                    // comment or processing instruction
)

type xmlNode struct {
	kind xmlNodeKind
	text string
	elem *xmlElement
}

type xmlElement struct {
	Name  xmlName
	Attrs []xmlAttr
	Nodes []xmlNode
}

// errXML wraps every load failure, so callers can treat them uniformly the way
// the C# caught XmlException.
var errXML = errors.New("xml")

func xmlErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errXML, fmt.Sprintf(format, args...))
}

// directText is the element's own leading text, equivalent to
// ElementTree's .text: everything between the start tag and the first child
// node that is not text. ok is false when there is none.
//
// Deliberately not the concatenation of all descendant text. For a nested node
// such as <dc:description><rdf:Alt>...</rdf:Alt></dc:description> the Python
// original sees no text at all, and so must we. Like the C# (whose XDocument
// keeps comments as nodes), a comment or processing instruction also ends the
// run.
func (e *xmlElement) directText() (string, bool) {
	var sb strings.Builder
	ok := false
	for _, n := range e.Nodes {
		if n.kind != xmlTextNode {
			break
		}
		sb.WriteString(n.text)
		ok = true
	}
	return sb.String(), ok
}

// elements returns the element children.
func (e *xmlElement) elements() []*xmlElement {
	var out []*xmlElement
	for _, n := range e.Nodes {
		if n.kind == xmlElemNode {
			out = append(out, n.elem)
		}
	}
	return out
}

// descendantsAndSelf walks the tree in document order.
func (e *xmlElement) descendantsAndSelf(visit func(*xmlElement)) {
	visit(e)
	for _, n := range e.Nodes {
		if n.kind == xmlElemNode {
			n.elem.descendantsAndSelf(visit)
		}
	}
}

func isXMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

func checkXMLChars(s string) error {
	for i, r := range s {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(s[i:]); size == 1 {
				return xmlErrorf("invalid UTF-8")
			}
		}
		if !isXMLChar(r) {
			return xmlErrorf("illegal character %U", r)
		}
	}
	return nil
}

// maxXMLDepth caps element nesting. Real XMP packets nest under a dozen
// levels; the cap keeps a hostile packet from making namespace lookup (which
// walks the open-element stack) quadratic and the tree walks recurse without
// bound.
const maxXMLDepth = 256

type rawFrame struct {
	raw  xml.Name // as written, prefix unresolved
	elem *xmlElement
	ns   map[string]string // prefix -> URI declared on this element
}

// loadXML parses text into a tree and returns its root element.
func loadXML(text string) (*xmlElement, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = true
	// The text is already decoded; an encoding="..." in the declaration is
	// irrelevant, as it was to XmlReader reading from a StringReader.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }

	var (
		root  *xmlElement
		stack []rawFrame
	)

	lookup := func(prefix string) (string, bool) {
		switch prefix {
		case "xml":
			return xmlNamespaceURI, true
		case "xmlns":
			return xmlnsNamespaceURI, true
		}
		for i := len(stack) - 1; i >= 0; i-- {
			if uri, ok := stack[i].ns[prefix]; ok {
				return uri, true
			}
		}
		// The default namespace is empty unless declared.
		return "", prefix == ""
	}

	for {
		start := dec.InputOffset()
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errXML, err)
		}
		end := dec.InputOffset()

		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 && root != nil {
				return nil, xmlErrorf("multiple root elements")
			}
			if len(stack) >= maxXMLDepth {
				return nil, xmlErrorf("elements nested deeper than %d", maxXMLDepth)
			}
			if err := checkQName(t.Name); err != nil {
				return nil, err
			}

			frame := rawFrame{raw: t.Name, ns: map[string]string{}}
			rawTag := text[start:end]
			attrs, err := normalizedAttrs(t.Attr, rawTag)
			if err != nil {
				return nil, err
			}

			// Namespace declarations first: they apply to the element's own
			// name and attributes.
			for _, a := range attrs {
				switch {
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					if a.Value == xmlNamespaceURI || a.Value == xmlnsNamespaceURI {
						return nil, xmlErrorf("reserved namespace bound as default")
					}
					frame.ns[""] = a.Value
				case a.Name.Space == "xmlns":
					if err := checkNSDecl(a.Name.Local, a.Value); err != nil {
						return nil, err
					}
					frame.ns[a.Name.Local] = a.Value
				}
			}
			stack = append(stack, frame)

			if t.Name.Space == "xmlns" {
				return nil, xmlErrorf("element name may not use the xmlns prefix")
			}
			elemNS, ok := lookup(t.Name.Space)
			if !ok {
				return nil, xmlErrorf("unbound prefix %q", t.Name.Space)
			}
			elem := &xmlElement{Name: xmlName{Space: elemNS, Local: t.Name.Local}}

			seenRaw := map[xml.Name]bool{}
			seen := map[xmlName]bool{}
			for _, a := range attrs {
				if err := checkQName(a.Name); err != nil {
					return nil, err
				}
				if seenRaw[a.Name] {
					return nil, xmlErrorf("duplicate attribute %s:%s", a.Name.Space, a.Name.Local)
				}
				seenRaw[a.Name] = true

				var name xmlName
				switch {
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					// XAttribute names a default declaration plain "xmlns".
					name = xmlName{Local: "xmlns"}
				case a.Name.Space == "":
					// Unprefixed attributes are in no namespace, not the
					// default one.
					name = xmlName{Local: a.Name.Local}
				default:
					uri, ok := lookup(a.Name.Space)
					if !ok {
						return nil, xmlErrorf("unbound prefix %q", a.Name.Space)
					}
					name = xmlName{Space: uri, Local: a.Name.Local}
				}
				if seen[name] {
					return nil, xmlErrorf("duplicate attribute {%s}%s", name.Space, name.Local)
				}
				seen[name] = true
				elem.Attrs = append(elem.Attrs, xmlAttr{Name: name, Value: a.Value})
			}

			stack[len(stack)-1].elem = elem
			if len(stack) == 1 {
				root = elem
			} else {
				parent := stack[len(stack)-2].elem
				parent.Nodes = append(parent.Nodes, xmlNode{kind: xmlElemNode, elem: elem})
			}

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, xmlErrorf("unexpected end element")
			}
			top := stack[len(stack)-1]
			if top.raw != t.Name {
				return nil, xmlErrorf("element <%s> closed by </%s>", qname(top.raw), qname(t.Name))
			}
			stack = stack[:len(stack)-1]

		case xml.CharData:
			s := string(t)
			isCDATA := strings.HasPrefix(text[start:end], "<![CDATA[")
			if !isCDATA && strings.Contains(text[start:end], "]]>") {
				return nil, xmlErrorf("']]>' not allowed in content")
			}
			if err := checkXMLChars(s); err != nil {
				return nil, err
			}
			if len(stack) == 0 {
				if isCDATA || strings.TrimFunc(s, isXMLSpace) != "" {
					return nil, xmlErrorf("text outside the root element")
				}
				continue
			}
			parent := stack[len(stack)-1].elem
			parent.Nodes = append(parent.Nodes, xmlNode{kind: xmlTextNode, text: s})

		case xml.Comment:
			if bytes.Contains(t, []byte("--")) || bytes.HasSuffix(t, []byte("-")) {
				return nil, xmlErrorf("'--' not allowed in a comment")
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1].elem
				parent.Nodes = append(parent.Nodes, xmlNode{kind: xmlOtherNode})
			}

		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml") && (t.Target != "xml" || start != 0) {
				return nil, xmlErrorf("misplaced XML declaration")
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1].elem
				parent.Nodes = append(parent.Nodes, xmlNode{kind: xmlOtherNode})
			}

		case xml.Directive:
			// DOCTYPE (prohibited) or a stray markup declaration.
			return nil, xmlErrorf("DTD is prohibited")
		}
	}

	if len(stack) != 0 {
		return nil, xmlErrorf("unexpected end of document")
	}
	if root == nil {
		return nil, xmlErrorf("root element is missing")
	}
	return root, nil
}

func qname(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

// checkQName rejects names encoding/xml splits leniently but a namespace-aware
// parser refuses, such as "a:b:c" or ":a".
func checkQName(n xml.Name) error {
	if n.Local == "" || strings.Contains(n.Local, ":") {
		return xmlErrorf("invalid qualified name %q", qname(n))
	}
	return nil
}

func checkNSDecl(prefix, uri string) error {
	switch {
	case prefix == "xmlns":
		return xmlErrorf("the xmlns prefix cannot be declared")
	case prefix == "xml" && uri != xmlNamespaceURI:
		return xmlErrorf("the xml prefix cannot be rebound")
	case prefix != "xml" && (uri == xmlNamespaceURI || uri == xmlnsNamespaceURI):
		return xmlErrorf("reserved namespace bound to prefix %q", prefix)
	case uri == "":
		// Undeclaring a prefix is XML 1.1 only.
		return xmlErrorf("prefix %q bound to an empty namespace", prefix)
	}
	return nil
}

// normalizedAttrs applies attribute-value normalization. encoding/xml has
// already decoded the values, which loses the difference between a literal
// newline (normalized to a space) and &#xA; (kept), so any value that holds
// tab/CR/LF is re-derived from the raw start tag.
func normalizedAttrs(attrs []xml.Attr, rawTag string) ([]xml.Attr, error) {
	needRaw := false
	for _, a := range attrs {
		if err := checkXMLChars(a.Value); err != nil {
			return nil, err
		}
		if strings.ContainsAny(a.Value, "\t\r\n") {
			needRaw = true
		}
	}
	if !needRaw {
		return attrs, nil
	}

	raw := rawAttrValues(rawTag)
	if len(raw) != len(attrs) {
		return nil, xmlErrorf("could not re-read attributes of %q", rawTag)
	}
	out := make([]xml.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = a
		if strings.ContainsAny(a.Value, "\t\r\n") {
			v, err := normalizeAttrValue(raw[i])
			if err != nil {
				return nil, err
			}
			out[i].Value = v
		}
	}
	return out, nil
}

// rawAttrValues returns the undecoded attribute values of a start tag, in
// order. The tag has already been accepted by encoding/xml, so this only has
// to find the quoted values, which cannot contain their own quote character.
func rawAttrValues(tag string) []string {
	var out []string
	i := strings.IndexAny(tag, " \t\r\n") // skip "<name"
	if i < 0 {
		return nil
	}
	for i < len(tag) {
		eq := strings.IndexByte(tag[i:], '=')
		if eq < 0 {
			break
		}
		i += eq + 1
		for i < len(tag) && isXMLSpace(rune(tag[i])) {
			i++
		}
		if i >= len(tag) {
			break
		}
		quote := tag[i]
		if quote != '"' && quote != '\'' {
			break
		}
		closeAt := strings.IndexByte(tag[i+1:], quote)
		if closeAt < 0 {
			break
		}
		out = append(out, tag[i+1:i+1+closeAt])
		i += closeAt + 2
	}
	return out
}

// normalizeAttrValue performs XML 1.0 section 3.3.3 normalization on a raw
// (undecoded) CDATA attribute value: line endings are first unified, each
// literal whitespace character becomes a space, then references are expanded.
func normalizeAttrValue(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, raw)

	var sb strings.Builder
	for {
		amp := strings.IndexByte(raw, '&')
		if amp < 0 {
			sb.WriteString(raw)
			return sb.String(), nil
		}
		sb.WriteString(raw[:amp])
		semi := strings.IndexByte(raw[amp:], ';')
		if semi < 0 {
			return "", xmlErrorf("unterminated reference")
		}
		ref := raw[amp+1 : amp+semi]
		raw = raw[amp+semi+1:]

		switch ref {
		case "lt":
			sb.WriteByte('<')
		case "gt":
			sb.WriteByte('>')
		case "amp":
			sb.WriteByte('&')
		case "apos":
			sb.WriteByte('\'')
		case "quot":
			sb.WriteByte('"')
		default:
			var n uint64
			var err error
			switch {
			case strings.HasPrefix(ref, "#x"):
				n, err = strconv.ParseUint(ref[2:], 16, 32)
			case strings.HasPrefix(ref, "#"):
				n, err = strconv.ParseUint(ref[1:], 10, 32)
			default:
				return "", xmlErrorf("undefined entity &%s;", ref)
			}
			if err != nil || !isXMLChar(rune(n)) {
				return "", xmlErrorf("invalid character reference &%s;", ref)
			}
			sb.WriteRune(rune(n))
		}
	}
}
