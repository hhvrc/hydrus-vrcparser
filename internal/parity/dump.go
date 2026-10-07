package parity

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hhvrc/hydrus-vrcparser/internal/store"
	"github.com/hhvrc/hydrus-vrcparser/internal/vrchat"
)

// chunkRow is one itxt_chunks row. Keyword and content type keep NULL
// distinct from empty because the dump reports them verbatim.
type chunkRow struct {
	fileID      int64
	seq         int64
	keyword     sql.NullString
	text        sql.NullString
	contentType sql.NullString
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

// readChunks streams every cached iTXt chunk ordered by (file_id, seq), the
// order the reference dumps use. The database is opened read-only through the
// store, which leaves a read-only database's schema alone.
func readChunks(ctx context.Context, dbPath string, visit func(chunkRow) error) error {
	db, err := store.OpenReadOnly(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.SQL().QueryContext(ctx, `SELECT file_id, seq, keyword, text, content_type
		FROM itxt_chunks ORDER BY file_id, seq`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var r chunkRow
		if err := rows.Scan(&r.fileID, &r.seq, &r.keyword, &r.text, &r.contentType); err != nil {
			return err
		}
		if err := visit(r); err != nil {
			return err
		}
	}
	return rows.Err()
}

// writeLines creates outPath and hands the caller a line writer. Lines end in
// "\n" on every platform; the comparers, being JSON readers, would not mind
// "\r\n" either.
func writeLines(outPath string, fill func(writeLine func(string) error) error) (err error) {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	w := bufio.NewWriter(f)
	if err := fill(func(line string) error {
		_, err := w.WriteString(line + "\n")
		return err
	}); err != nil {
		return err
	}
	return w.Flush()
}

// DumpChunkParse writes per-chunk parse results for every cached iTXt chunk.
//
// It operates one chunk at a time on purpose: that isolates parser behaviour
// from the file-level priority contest, so a mismatch points at a specific
// parser rather than at the dispatch logic.
func DumpChunkParse(ctx context.Context, dbPath, outPath string) error {
	written := 0
	err := writeLines(outPath, func(writeLine func(string) error) error {
		return readChunks(ctx, dbPath, func(c chunkRow) error {
			written++
			return writeLine(chunkRecord(c))
		})
	})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d chunk records to %s\n", written, outPath)
	return nil
}

func chunkRecord(c chunkRow) string {
	rawText := vrchat.SanitizeITXt(c.text.String)

	// Anything not json/xml is attempted as line format. Unlike the loader,
	// the dumper does not sniff an empty content type for XMP, so the dump
	// isolates the parsers from the loader's dispatch.
	ctype := strings.ToLower(c.contentType.String)
	if ctype != vrchat.ContentTypeJSON && ctype != vrchat.ContentTypeXML {
		ctype = vrchat.ContentTypeLine
	}

	meta, editor, effective, perr := vrchat.ParseChunk(rawText, ctype)

	// Keys in ordinal order, the order the reference dumps hold them in.
	var w jsonWriter
	w.raw("{")
	if perr != nil {
		w.key("error", true)
		w.str(exceptionName(perr))
		w.key("file_id", false)
		w.raw(strconv.FormatInt(c.fileID, 10))
		w.key("keyword", false)
		w.strOrNull(nullable(c.keyword))
		w.key("seq", false)
		w.raw(strconv.FormatInt(c.seq, 10))
		w.key("stored_type", false)
		w.strOrNull(nullable(c.contentType))
		w.raw("}")
		return w.sb.String()
	}

	w.key("author_id", true)
	w.str(meta.Author.ID)
	w.key("author_name", false)
	w.str(meta.Author.DisplayName)
	w.key("created", false)
	w.strOrNull(formatCreated(meta.Created))
	w.key("creator_tool", false)
	w.strOrNull(meta.CreatorTool)
	w.key("editor_software", false)
	w.strArray(editor)
	w.key("effective_type", false)
	w.str(effective)
	w.key("file_id", false)
	w.raw(strconv.FormatInt(c.fileID, 10))
	w.key("instance_id", false)
	w.str(meta.World.InstanceID)
	w.key("keyword", false)
	w.strOrNull(nullable(c.keyword))
	w.key("players", false)
	w.raw("[")
	for i, p := range meta.Players {
		if i > 0 {
			w.raw(",")
		}
		w.strArray([]string{p.ID, p.DisplayName})
	}
	w.raw("]")
	w.key("seq", false)
	w.raw(strconv.FormatInt(c.seq, 10))
	w.key("stored_type", false)
	w.strOrNull(nullable(c.contentType))
	w.key("world_id", false)
	w.str(meta.World.ID)
	w.key("world_name", false)
	w.str(meta.World.Name)
	w.raw("}")
	return w.sb.String()
}

// formatCreated renders like Python's datetime.isoformat(): microseconds only
// when the value has a sub-second part (the 7th tick digit is truncated, as
// the original dumps did), and a +HH:MM offset.
func formatCreated(t *time.Time) *string {
	if t == nil {
		return nil
	}
	layout := "2006-01-02T15:04:05-07:00"
	if t.Nanosecond() != 0 {
		layout = "2006-01-02T15:04:05.000000-07:00"
	}
	s := t.Format(layout)
	return &s
}

// exceptionName maps to the Python exception name the reference dump records.
func exceptionName(err error) string {
	var (
		lineErr *vrchat.MetaParseError
		xmpErr  *vrchat.XMPParseError
		jsonErr *vrchat.JSONError
	)
	switch {
	case errors.As(err, &lineErr):
		return "MetaParseError"
	case errors.As(err, &xmpErr):
		return "XMPParseError"
	case errors.As(err, &jsonErr):
		return "JSONDecodeError"
	}
	return fmt.Sprintf("%T", err)
}

// DumpFileTags writes the tags the VRChat tagger derives for every file that
// has cached iTXt chunks, so the whole pipeline (priority contest, editor
// provenance, tag building) can be diffed. The tags come from
// vrchat.FileTags, the same path the tagger's Derive takes.
//
// Per file rather than per chunk, unlike DumpChunkParse: this is the output
// that actually reaches Hydrus.
func DumpFileTags(ctx context.Context, dbPath, outPath string) error {
	written, tagged := 0, 0
	err := writeLines(outPath, func(writeLine func(string) error) error {
		var (
			current int64
			group   []store.Chunk
			have    bool
		)
		flush := func() error {
			if !have {
				return nil
			}
			written++
			tags := vrchat.FileTags(group)

			var w jsonWriter
			w.raw("{")
			w.key("file_id", true)
			w.raw(strconv.FormatInt(current, 10))
			if tags.Empty() {
				// No chunk yielded VRChat metadata (a file with metadata always
				// has at least the "vrchat" tag). Recorded explicitly so every
				// file has a key in both dumps.
				w.key("tags", false)
				w.raw("null")
				w.key("tag_hash", false)
				w.raw("null")
			} else {
				tagged++
				w.key("tags", false)
				w.strArray(tags.Sorted())
				w.key("tag_hash", false)
				w.str(tags.Hash())
			}
			w.raw("}")
			return writeLine(w.sb.String())
		}

		err := readChunks(ctx, dbPath, func(c chunkRow) error {
			if have && c.fileID != current {
				if err := flush(); err != nil {
					return err
				}
				group = group[:0]
			}
			current, have = c.fileID, true
			group = append(group, store.Chunk{
				Seq:         int(c.seq),
				Keyword:     nullable(c.keyword),
				Text:        nullable(c.text),
				ContentType: c.contentType.String,
			})
			return nil
		})
		if err != nil {
			return err
		}
		return flush()
	})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d file records (%d with tags) to %s\n", written, tagged, outPath)
	return nil
}
