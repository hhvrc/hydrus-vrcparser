package vrchat

import "time"

// Metadata is the common schema all three VRChat metadata formats normalize
// to: the shape of the original Python normalizer's dict.
//
// String fields default to empty rather than absent, matching the Python
// normalizer, so tag building can test them uniformly with a trim-and-check.
// The few fields where absent and empty are observably different (they are
// reported separately by the parity dump) are pointers.
type Metadata struct {
	RawText string

	// Type is "screenshotmanager", "lfs", "xmp", or whatever a JSON payload's
	// "type" string holds; nil for VRCX JSON, which carries none.
	Type *string

	// Index is the legacy line format's index, or a JSON payload's integer
	// "index".
	Index *int

	// CreatorTool is xmp:CreatorTool when the source was XMP, or a JSON
	// payload's "creator_tool"; nil when absent.
	CreatorTool *string

	Author   Author
	World    World
	Position Position

	// Rq is the render quality.
	Rq int

	Players []Player

	// Created is the creation timestamp, in the offset it was written with.
	// Only XMP carries one.
	Created *time.Time

	// EditorSoftware lists apps that created or edited the image, from XMP
	// provenance.
	EditorSoftware []string
}

// Author is the user who took the screenshot.
type Author struct {
	ID          string
	DisplayName string
}

// World is the world (and instance) the screenshot was taken in.
type World struct {
	ID         string
	InstanceID string
	Name       string
}

// Position is a point in world space.
type Position struct {
	X, Y, Z float64
}

// Player is another user present in the instance.
type Player struct {
	ID          string
	DisplayName string
	Position    Position
}

func strPtr(s string) *string { return &s }
