package vrchat

import (
	"encoding/json"
)

// Normalizers map each source format onto Metadata. The original Python did
// this in its data layer; here it lives with the schema.
//
// Field-level tolerance is the point: a record with an unparseable position
// still yields its author and world tags. Missing strings normalize to empty
// rather than absent so tag building can test them uniformly.

// NormalizeJSON normalizes a decoded VRCX JSON payload. root is a generic
// encoding/json tree decoded with UseNumber (map[string]any, []any, string,
// json.Number, bool, nil); a non-object root yields empty metadata, since
// json.loads accepted bare arrays and scalars too.
func NormalizeJSON(root any, rawText string) *Metadata {
	meta := &Metadata{RawText: rawText, Players: []Player{}}

	obj, ok := root.(map[string]any)
	if !ok {
		return meta
	}

	meta.Type = jsonStringOrNil(obj, "type")
	if n, ok := obj["index"].(json.Number); ok {
		if i, ok := jsonInt32(n); ok {
			meta.Index = &i
		}
	}
	meta.CreatorTool = jsonStringOrNil(obj, "creator_tool")

	if author, ok := jsonObject(obj, "author"); ok {
		meta.Author = Author{
			ID: jsonStringOrEmpty(author, "id"),
			// VRCX writes displayName; some older payloads use name.
			DisplayName: firstNonEmpty(jsonStringOrEmpty(author, "displayName"), jsonStringOrEmpty(author, "name")),
		}
	}

	if world, ok := jsonObject(obj, "world"); ok {
		meta.World = World{
			ID:         jsonStringOrEmpty(world, "id"),
			InstanceID: jsonStringOrEmpty(world, "instanceId"),
			Name:       jsonStringOrEmpty(world, "name"),
		}
	}

	if position, ok := jsonObject(obj, "position"); ok {
		meta.Position = jsonPosition(position)
	}

	meta.Rq = jsonRq(obj)

	if players, ok := obj["players"].([]any); ok {
		for _, entry := range players {
			// The Python passed the raw list straight through, so a non-object
			// entry would later crash tag building with an AttributeError it
			// did not catch. Skipping is strictly safer and cannot change the
			// result for well-formed data.
			p, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			player := Player{
				ID:          jsonStringOrEmpty(p, "id"),
				DisplayName: firstNonEmpty(jsonStringOrEmpty(p, "displayName"), jsonStringOrEmpty(p, "name")),
			}
			if pp, ok := jsonObject(p, "position"); ok {
				player.Position = jsonPosition(pp)
			}
			meta.Players = append(meta.Players, player)
		}
	}

	return meta
}

// NormalizeXMP normalizes a native VRChat XMP packet.
func NormalizeXMP(xmp *XMPMetadata, rawText string) *Metadata {
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return &Metadata{
		RawText:     rawText,
		Type:        strPtr("xmp"),
		CreatorTool: xmp.CreatorTool,
		Author: Author{
			ID:          deref(xmp.AuthorID),
			DisplayName: deref(xmp.AuthorDisplayName),
		},
		World: World{
			ID: deref(xmp.WorldID),
			// XMP carries no instance id.
			InstanceID: "",
			Name:       deref(xmp.WorldName),
		},
		Players: []Player{},
		Created: xmp.Created,
	}
}

func jsonPosition(obj map[string]any) Position {
	return Position{
		X: jsonDoubleOrZero(obj, "x"),
		Y: jsonDoubleOrZero(obj, "y"),
		Z: jsonDoubleOrZero(obj, "z"),
	}
}

func jsonRq(obj map[string]any) int {
	switch v := obj["rq"].(type) {
	case json.Number:
		// Python int() truncates toward zero.
		if d, ok := jsonDouble(v); ok {
			return truncToInt(d)
		}
	case string:
		// Python int("5") works; int("5.7") raises and leaves the default.
		if i, ok := parseDotnetInt32(v); ok {
			return i
		}
	}
	return 0
}

func jsonDoubleOrZero(obj map[string]any, key string) float64 {
	switch v := obj[key].(type) {
	case json.Number:
		if d, ok := jsonDouble(v); ok {
			return d
		}
	case string:
		if d, ok := parseDotnetDouble(v, false); ok {
			return d
		}
	}
	return 0
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
