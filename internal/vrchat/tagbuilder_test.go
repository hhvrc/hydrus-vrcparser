package vrchat

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// Cases carried over from the original Python tag-builder tests.

func TestBuildFileTags(t *testing.T) {
	created := time.Date(2025, 8, 30, 6, 45, 33, 0, time.FixedZone("", 2*3600))
	tests := []struct {
		name        string
		meta        Metadata
		contains    []string
		notPrefixed []string
		exact       []string
	}{
		{
			name: "author and world",
			meta: Metadata{
				Author: Author{ID: "usr_abc", DisplayName: "TestUser"},
				World:  World{ID: "wrld_xyz", Name: "MyWorld"},
			},
			contains: []string{
				"vrchat", "vrchat-author-id:usr_abc", "vrchat-author-name:TestUser",
				"vrchat-world-id:wrld_xyz", "vrchat-world-name:MyWorld",
			},
		},
		{
			name:     "creator tool verbatim",
			meta:     Metadata{CreatorTool: strPtr("VRChat")},
			contains: []string{"creator_tool:VRChat"},
		},
		{
			name: "version noise kept in creator_tool but not editor",
			meta: Metadata{CreatorTool: strPtr("Microsoft Windows Photo Viewer 10.0.26100.1882")},
			contains: []string{
				"creator_tool:Microsoft Windows Photo Viewer 10.0.26100.1882",
				"editor:microsoft windows photo viewer",
			},
		},
		{
			name:        "no creator tool",
			meta:        Metadata{Author: Author{ID: "usr_abc"}},
			notPrefixed: []string{"creator_tool:"},
		},
		{
			name:     "editor tags from editor software",
			meta:     Metadata{EditorSoftware: []string{"Adobe Photoshop Express (Android)"}},
			contains: []string{"editor:adobe", "editor:adobe photoshop express"},
		},
		{
			// VRChat is the source game, not an external editor.
			name:        "VRChat is never an editor",
			meta:        Metadata{CreatorTool: strPtr("VRChat"), EditorSoftware: []string{"VRChat"}},
			notPrefixed: []string{"editor:"},
		},
		{
			name: "every player",
			meta: Metadata{Players: []Player{
				{ID: "usr_p1", DisplayName: "Player1"}, {ID: "usr_p2", DisplayName: "Player2"},
			}},
			contains: []string{"vrchat-user-id:usr_p1", "vrchat-user-name:Player1", "vrchat-user-id:usr_p2"},
		},
		{
			// 06:45 at +02:00 is still the 30th locally; converting to UTC
			// first would be a different day for late-evening screenshots.
			name:     "date in the timestamp's own offset",
			meta:     Metadata{Created: &created},
			contains: []string{"vrchat-date:2025-08-30"},
		},
		{
			name:        "no timestamp, no date",
			meta:        Metadata{Author: Author{ID: "usr_abc"}},
			notPrefixed: []string{"vrchat-date:"},
		},
		{
			name:  "empty metadata yields only the marker",
			meta:  Metadata{},
			exact: []string{"vrchat"},
		},
		{
			name:     "instance id",
			meta:     Metadata{World: World{ID: "wrld_abc", InstanceID: "wrld_abc:12345"}},
			contains: []string{"vrchat-world-instanceId:wrld_abc:12345"},
		},
		{
			// An image whose only XMP is "GIMP 2.10" still earns editor:gimp.
			name:     "creator tool counts as editor provenance",
			meta:     Metadata{CreatorTool: strPtr("GIMP 2.10.34")},
			contains: []string{"creator_tool:GIMP 2.10.34", "editor:gimp"},
		},
		{
			name: "builder order",
			meta: Metadata{
				Author:         Author{ID: " usr_a ", DisplayName: "A"},
				World:          World{ID: "wrld_w", Name: "W", InstanceID: "wrld_w:1"},
				Players:        []Player{{ID: "usr_p", DisplayName: "  "}},
				CreatorTool:    strPtr("GIMP 2.10"),
				EditorSoftware: []string{"Adobe Photoshop 25.0"},
				Created:        &created,
			},
			exact: []string{
				"vrchat", "vrchat-author-id:usr_a", "vrchat-author-name:A",
				"vrchat-world-id:wrld_w", "vrchat-world-name:W", "vrchat-world-instanceId:wrld_w:1",
				"vrchat-user-id:usr_p", "creator_tool:GIMP 2.10",
				"editor:adobe", "editor:adobe photoshop", "editor:gimp",
				"vrchat-date:2025-08-30",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := BuildFileTags(&tt.meta)
			for _, want := range tt.contains {
				if !slices.Contains(tags, want) {
					t.Errorf("missing %q in %q", want, tags)
				}
			}
			for _, prefix := range tt.notPrefixed {
				for _, tag := range tags {
					if strings.HasPrefix(tag, prefix) {
						t.Errorf("unexpected %q", tag)
					}
				}
			}
			if tt.exact != nil && !slices.Equal(tags, tt.exact) {
				t.Errorf("got %q, want %q", tags, tt.exact)
			}
		})
	}
}

func TestBuildEditorTags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"brand and full app name", []string{"Adobe Photoshop Express (Android)"},
			[]string{"editor:adobe", "editor:adobe photoshop express"}},
		{"trailing version stripped", []string{"GIMP 2.10.34"}, []string{"editor:gimp"}},
		{"photoshop maps to adobe", []string{"Adobe Photoshop 25.0"},
			[]string{"editor:adobe", "editor:adobe photoshop"}},
		{"VRChat skipped", []string{"VRChat"}, []string{}},
		{"unknown editor by app name alone", []string{"SomeRandomTool"}, []string{"editor:somerandomtool"}},
		{"de-duplicated across inputs",
			[]string{"Adobe Photoshop Express (Android)", "Adobe Photoshop Express (Android)"},
			[]string{"editor:adobe", "editor:adobe photoshop express"}},
		{"no inputs", nil, []string{}},
		{"empty and blank inputs", []string{"", "   "}, []string{}},
		{"v-prefixed version", []string{"Snapseed v2.19"}, []string{"editor:snapseed"}},
		{"whitespace collapsed", []string{"Paint.NET   Pro"}, []string{"editor:paint.net", "editor:paint.net pro"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildEditorTags(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
