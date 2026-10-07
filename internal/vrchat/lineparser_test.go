package vrchat

import (
	"errors"
	"testing"
)

// Cases carried over from the original Python line-parser tests.

func mustParseLine(t *testing.T, line string) *Metadata {
	t.Helper()
	m, err := ParseMetaLine(line)
	if err != nil {
		t.Fatalf("ParseMetaLine(%q): %v", line, err)
	}
	return m
}

func TestParseMetaLine(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		check func(t *testing.T, m *Metadata)
	}{
		{
			name: "screenshotmanager basics",
			line: "screenshotmanager|0|author:usr_abc,TestUser|world:wrld_123,inst1,MyWorld",
			check: func(t *testing.T, m *Metadata) {
				if *m.Type != "screenshotmanager" || *m.Index != 0 {
					t.Errorf("type/index = %q/%d", *m.Type, *m.Index)
				}
				if m.Author != (Author{ID: "usr_abc", DisplayName: "TestUser"}) {
					t.Errorf("author = %+v", m.Author)
				}
				if m.World.ID != "wrld_123" || m.World.Name != "MyWorld" {
					t.Errorf("world = %+v", m.World)
				}
			},
		},
		{
			name: "lfs type",
			line: "lfs|5|author:usr_xyz,User2",
			check: func(t *testing.T, m *Metadata) {
				if *m.Type != "lfs" || *m.Index != 5 {
					t.Errorf("type/index = %q/%d", *m.Type, *m.Index)
				}
			},
		},
		{
			name: "position",
			line: "screenshotmanager|0|pos:1.5,2.0,3.5",
			check: func(t *testing.T, m *Metadata) {
				if m.Position != (Position{1.5, 2.0, 3.5}) {
					t.Errorf("position = %+v", m.Position)
				}
			},
		},
		{
			name: "render quality",
			line: "screenshotmanager|0|rq:4",
			check: func(t *testing.T, m *Metadata) {
				if m.Rq != 4 {
					t.Errorf("rq = %d", m.Rq)
				}
			},
		},
		{
			name: "players",
			line: "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;usr_p2,4.0,5.0,6.0,Player2",
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 2 {
					t.Fatalf("players = %+v", m.Players)
				}
				if m.Players[0].ID != "usr_p1" || m.Players[0].DisplayName != "Player1" {
					t.Errorf("player 0 = %+v", m.Players[0])
				}
				if m.Players[1].Position != (Position{4, 5, 6}) {
					t.Errorf("player 1 position = %+v", m.Players[1].Position)
				}
			},
		},
		{
			// Lenient by design: one bad field must not discard the whole record.
			name: "malformed author keeps the record",
			line: "screenshotmanager|0|author:no_comma",
			check: func(t *testing.T, m *Metadata) {
				if m.Author.ID != "" || *m.Type != "screenshotmanager" {
					t.Errorf("author = %+v, type = %q", m.Author, *m.Type)
				}
			},
		},
		{
			name: "malformed position keeps the record",
			line: "screenshotmanager|0|pos:1.0,2.0",
			check: func(t *testing.T, m *Metadata) {
				if m.Position.X != 0 {
					t.Errorf("position = %+v", m.Position)
				}
			},
		},
		{
			name: "malformed render quality keeps the record",
			line: "screenshotmanager|0|rq:abc",
			check: func(t *testing.T, m *Metadata) {
				if m.Rq != 0 {
					t.Errorf("rq = %d", m.Rq)
				}
			},
		},
		{
			name: "world with too few parts is skipped",
			line: "screenshotmanager|0|world:wrld_abc,12345",
			check: func(t *testing.T, m *Metadata) {
				if m.World.ID != "" {
					t.Errorf("world = %+v", m.World)
				}
			},
		},
		{
			name: "unknown keys are ignored",
			line: "screenshotmanager|0|future_field:some_value",
			check: func(t *testing.T, m *Metadata) {
				if *m.Type != "screenshotmanager" {
					t.Errorf("type = %q", *m.Type)
				}
			},
		},
		{
			name: "instance id is prefixed with the world id",
			line: "screenshotmanager|0|world:wrld_abc,12345,Test World",
			check: func(t *testing.T, m *Metadata) {
				if m.World.InstanceID != "wrld_abc:12345" {
					t.Errorf("instance = %q", m.World.InstanceID)
				}
			},
		},
		{
			// Older screenshotmanager output omits the "world:" key entirely.
			name: "bare world segment",
			line: "screenshotmanager|0|wrld_abc,12345,Test World",
			check: func(t *testing.T, m *Metadata) {
				want := World{ID: "wrld_abc", InstanceID: "wrld_abc:12345", Name: "Test World"}
				if m.World != want {
					t.Errorf("world = %+v, want %+v", m.World, want)
				}
			},
		},
		{
			name: "malformed player entries are skipped",
			line: "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;bad_entry;usr_p2,4.0,5.0,6.0,Player2",
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 2 || m.Players[0].ID != "usr_p1" || m.Players[1].ID != "usr_p2" {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
		{
			name: "players with non-numeric coordinates are skipped",
			line: "screenshotmanager|0|players:usr_p1,x,y,z,Player1;usr_p2,1.0,2.0,3.0,Player2",
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 1 || m.Players[0].ID != "usr_p2" {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
		{
			// Taken verbatim from a content_type='line' row in vrchat.db: the
			// world segment is bare and the display name contains a space.
			name: "exact live database shape",
			line: "screenshotmanager|0|author:usr_89459bcc-2790-4805-acd7-819e7c28618c,Max Cheetos" +
				"|wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f,60145,Crimson Moon",
			check: func(t *testing.T, m *Metadata) {
				if m.Author != (Author{ID: "usr_89459bcc-2790-4805-acd7-819e7c28618c", DisplayName: "Max Cheetos"}) {
					t.Errorf("author = %+v", m.Author)
				}
				want := World{
					ID:         "wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f",
					InstanceID: "wrld_e5cfc2ef-3c37-4952-ab6b-7cce296b124f:60145",
					Name:       "Crimson Moon",
				}
				if m.World != want {
					t.Errorf("world = %+v", m.World)
				}
			},
		},
		{
			// World is split into at most 3 parts, so the name keeps its commas.
			name: "commas inside a world name",
			line: "screenshotmanager|0|world:wrld_abc,1,A, B, and C",
			check: func(t *testing.T, m *Metadata) {
				if m.World.Name != "A, B, and C" {
					t.Errorf("name = %q", m.World.Name)
				}
			},
		},
		{
			name: "commas inside a player display name",
			line: "screenshotmanager|0|players:usr_p1,1,2,3,Smith, Jr.",
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 1 || m.Players[0].DisplayName != "Smith, Jr." {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
		{
			// .NET's number parser tolerates surrounding whitespace; strconv
			// alone does not.
			name: "index and coordinates with surrounding whitespace",
			line: "lfs|7 |players:usr_p1, 1.5 ,2,3,Name",
			check: func(t *testing.T, m *Metadata) {
				if *m.Index != 7 || len(m.Players) != 1 || m.Players[0].Position.X != 1.5 {
					t.Errorf("index = %d, players = %+v", *m.Index, m.Players)
				}
			},
		},
		{
			// strconv accepts hex floats; .NET does not, so the player is dropped.
			name: "hex float coordinates are rejected",
			line: "lfs|0|players:usr_p1,0x1p3,2,3,Name",
			check: func(t *testing.T, m *Metadata) {
				if len(m.Players) != 0 {
					t.Errorf("players = %+v", m.Players)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, mustParseLine(t, tt.line))
		})
	}
}

func TestParseMetaLineRejectsUnrecoverableInput(t *testing.T) {
	for _, line := range []string{"unknown|0", "screenshotmanager", "screenshotmanager|abc", "lfs|99999999999"} {
		t.Run(line, func(t *testing.T) {
			_, err := ParseMetaLine(line)
			var perr *MetaParseError
			if !errors.As(err, &perr) {
				t.Errorf("ParseMetaLine(%q) error = %v, want *MetaParseError", line, err)
			}
		})
	}
}
