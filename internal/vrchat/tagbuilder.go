package vrchat

import (
	"regexp"
	"sort"
	"strings"
)

// editorBrands maps known image-editor vendors to a canonical brand tag,
// matched as a case-insensitive substring against XMP CreatorTool /
// softwareAgent. Order matters: the first match wins, so specific names
// precede generic vendors ("paintshop" before "corel" would be wrong,
// "photoshop" before "adobe" is not, since both yield "adobe").
var editorBrands = []struct{ needle, brand string }{
	{"adobe", "adobe"},
	{"photoshop", "adobe"},
	{"lightroom", "adobe"},
	{"gimp", "gimp"},
	{"affinity", "affinity"},
	{"serif", "affinity"},
	{"corel", "corel"},
	{"paintshop", "corel"},
	{"paint.net", "paint.net"},
	{"pixlr", "pixlr"},
	{"picsart", "picsart"},
	{"snapseed", "snapseed"},
	{"photoroom", "photoroom"},
	{"windows photo", "microsoft"},
	{"microsoft", "microsoft"},
}

// Tag namespaces, including the trailing colon. Other packages (correlate)
// read these tags back, so they are declared once, here.
const (
	NSAuthorID        = "vrchat-author-id:"
	NSAuthorName      = "vrchat-author-name:"
	NSWorldID         = "vrchat-world-id:"
	NSWorldName       = "vrchat-world-name:"
	NSWorldInstanceID = "vrchat-world-instanceId:"
	NSUserID          = "vrchat-user-id:"
	NSUserName        = "vrchat-user-name:"
	NSDate            = "vrchat-date:"
	NSCreatorTool     = "creator_tool:"
	NSEditor          = "editor:"
)

// BuildFileTags returns the Hydrus tags for one file. The order matches the
// original Python builder's, which the tag hash does not care about but a
// diff of the tag lists does.
func BuildFileTags(meta *Metadata) []string {
	tags := []string{"vrchat"}
	add := func(prefix, value string) {
		if v := strings.TrimSpace(value); v != "" {
			tags = append(tags, prefix+v)
		}
	}

	add(NSAuthorID, meta.Author.ID)
	add(NSAuthorName, meta.Author.DisplayName)

	add(NSWorldID, meta.World.ID)
	add(NSWorldName, meta.World.Name)
	add(NSWorldInstanceID, meta.World.InstanceID)

	for _, p := range meta.Players {
		add(NSUserID, p.ID)
		add(NSUserName, p.DisplayName)
	}

	creatorTool := ""
	if meta.CreatorTool != nil {
		creatorTool = strings.TrimSpace(*meta.CreatorTool)
	}
	add(NSCreatorTool, creatorTool)

	// The creator tool counts as editor provenance too: an image whose only
	// XMP is "GIMP 2.10" still deserves editor:gimp.
	software := make([]string, 0, len(meta.EditorSoftware)+1)
	if creatorTool != "" {
		software = append(software, creatorTool)
	}
	software = append(software, meta.EditorSoftware...)
	tags = append(tags, BuildEditorTags(software)...)

	if meta.Created != nil {
		// Formatted in the timestamp's own offset, never UTC.
		tags = append(tags, NSDate+meta.Created.Format("2006-01-02"))
	}

	return tags
}

// BuildEditorTags turns XMP creator/editor software strings into editor:
// tags, emitting both the brand and the full normalized app name: "Adobe
// Photoshop Express (Android)" gives editor:adobe and editor:adobe photoshop
// express. The result is de-duplicated and sorted.
//
// VRChat itself is skipped: it is the source game, not an external editor.
func BuildEditorTags(softwareStrings []string) []string {
	seen := map[string]bool{}
	tags := []string{}
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}

	for _, raw := range softwareStrings {
		if raw == "" {
			continue
		}

		low := strings.ToLower(raw)
		if strings.Contains(low, "vrchat") {
			continue
		}

		if app := normalizeAppName(raw); app != "" {
			add(NSEditor + app)
		}

		for _, b := range editorBrands {
			if strings.Contains(low, b.needle) {
				add(NSEditor + b.brand)
				break
			}
		}
	}

	// Bytewise order is code point order, which is Python's sorted() order.
	sort.Strings(tags)
	return tags
}

var (
	trailingParenthetical = regexp.MustCompile(`` + dotnetSpace + `*\([^)]*\)` + dotnetSpace + `*$`)
	trailingVersion       = regexp.MustCompile(dotnetSpace + `+v?\p{Nd}[\p{Nd}.\-]*$`)
	whitespaceRun         = regexp.MustCompile(dotnetSpace + `+`)
)

// dotnetSpace is .NET's regex \s: Unicode separators plus the ASCII and NEL
// control whitespace. RE2's \s and \d are ASCII-only, unlike .NET's.
const dotnetSpace = `[\f\n\r\t\v\x{85}\p{Z}]`

// normalizeAppName lowercases an app name and drops trailing
// platform/version noise: "Adobe Photoshop Express (Android)" -> "adobe
// photoshop express", "GIMP 2.10.34" -> "gimp".
func normalizeAppName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = trailingParenthetical.ReplaceAllString(s, "")
	s = trailingVersion.ReplaceAllString(s, "")
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(s, " "))
}
