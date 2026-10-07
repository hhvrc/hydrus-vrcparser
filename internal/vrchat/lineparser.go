package vrchat

import (
	"fmt"
	"strings"
)

// MetaParseError reports a metadata line that cannot be parsed at all. The
// parity dump records it as Python's "MetaParseError".
type MetaParseError struct {
	Msg string
}

func (e *MetaParseError) Error() string { return e.Msg }

func metaParseErrorf(format string, args ...any) error {
	return &MetaParseError{Msg: fmt.Sprintf(format, args...)}
}

// ParseMetaLine parses the legacy pipe-delimited "screenshotmanager" / "lfs"
// formats, as the original Python parser did.
//
// Parsing is deliberately lenient below the top level: a malformed individual
// field is skipped rather than discarding the whole record, because partial
// metadata still yields useful tags. Only a missing/unknown type or a
// non-integer index is fatal, and is reported as a *MetaParseError.
//
// Skipped fields are dropped silently (the original tools logged warnings
// that nothing acted on).
func ParseMetaLine(line string) (*Metadata, error) {
	meta := &Metadata{RawText: line, Players: []Player{}}

	parts := strings.Split(line, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	if len(parts) < 2 {
		return nil, metaParseErrorf("Line must have at least type and index: '%s'", line)
	}

	metaType := parts[0]
	if metaType != "screenshotmanager" && metaType != "lfs" {
		return nil, metaParseErrorf("Unknown meta type: '%s'", metaType)
	}
	meta.Type = strPtr(metaType)

	index, ok := parseDotnetInt32(parts[1])
	if !ok {
		return nil, metaParseErrorf("Invalid index (must be int): '%s'", parts[1])
	}
	meta.Index = &index

	for _, seg := range parts[2:] {
		// Older screenshotmanager output emits the world segment bare,
		// without the "world:" prefix.
		if strings.HasPrefix(seg, "wrld_") {
			parseLineWorld(seg, meta)
			continue
		}

		key, val, found := strings.Cut(seg, ":")
		if !found {
			continue
		}

		// Each field parser leaves meta untouched when its field is malformed.
		switch key {
		case "author":
			parseLineAuthor(val, meta)
		case "world":
			parseLineWorld(val, meta)
		case "pos":
			parseLinePosition(val, meta)
		case "rq":
			if rq, ok := parseDotnetInt32(val); ok {
				meta.Rq = rq
			}
		case "players":
			parseLinePlayers(val, meta)
		default:
			// Unknown keys are ignored.
		}
	}

	return meta, nil
}

func parseLineAuthor(val string, meta *Metadata) {
	id, name, found := strings.Cut(val, ",")
	if !found {
		return
	}
	meta.Author = Author{ID: id, DisplayName: name}
}

func parseLineWorld(val string, meta *Metadata) {
	// Split into at most 3: the world name may itself contain commas.
	fields := strings.SplitN(val, ",", 3)
	if len(fields) != 3 {
		return
	}
	meta.World = World{
		ID: fields[0],
		// Matches the VRCX JSON convention, where instanceId is prefixed with
		// the world id.
		InstanceID: fields[0] + ":" + fields[1],
		Name:       fields[2],
	}
}

func parseLinePosition(val string, meta *Metadata) {
	coords := strings.Split(val, ",")
	if len(coords) != 3 {
		return
	}
	x, okX := parseDotnetDouble(coords[0], true)
	y, okY := parseDotnetDouble(coords[1], true)
	z, okZ := parseDotnetDouble(coords[2], true)
	if !okX || !okY || !okZ {
		return
	}
	meta.Position = Position{X: x, Y: y, Z: z}
}

func parseLinePlayers(val string, meta *Metadata) {
	players := []Player{}

	for _, entry := range strings.Split(val, ";") {
		// Split into at most 5: a display name may contain commas.
		fields := strings.SplitN(entry, ",", 5)
		if len(fields) != 5 {
			continue
		}

		px, okX := parseDotnetDouble(fields[1], true)
		py, okY := parseDotnetDouble(fields[2], true)
		pz, okZ := parseDotnetDouble(fields[3], true)
		if !okX || !okY || !okZ {
			continue
		}

		players = append(players, Player{
			ID:          fields[0],
			DisplayName: fields[4],
			Position:    Position{X: px, Y: py, Z: pz},
		})
	}

	meta.Players = players
}
