package png

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParsesAnUncompressedChunk(t *testing.T) {
	r := ParseRecord(itxt("Description", itxtOpts{text: "hello world"}), 0)

	want := Record{Keyword: "Description", Text: "hello world"}
	if r != want {
		t.Fatalf("got %+v, want %+v", r, want)
	}
}

func TestPreservesANonZeroCompressionFlag(t *testing.T) {
	// Compressed iTXt is not inflated, but the flag must survive rather than
	// the chunk being silently dropped.
	r := ParseRecord(itxt("Description", itxtOpts{compFlag: 1, text: "compressed data"}), 0)

	if r.CompressionFlag != 1 || r.Text != "compressed data" {
		t.Fatalf("got %+v", r)
	}
}

func TestParsesLanguageAndTranslatedKeyword(t *testing.T) {
	r := ParseRecord(itxt("Description", itxtOpts{lang: "en", trans: "Desc", text: "content"}), 0)

	if r.LanguageTag != "en" || r.TranslatedKeyword != "Desc" || r.Text != "content" {
		t.Fatalf("got %+v", r)
	}
}

func TestKeepsNullBytesInsideTheTextSection(t *testing.T) {
	// Only the first two NULs after the flags are separators; the rest belong
	// to the text.
	r := ParseRecord(itxt("Description", itxtOpts{text: "before\x00after"}), 0)

	if r.Unparseable || r.Text != "before\x00after" {
		t.Fatalf("got %+v", r)
	}
}

func TestReportsUnparseableForMalformedPayloads(t *testing.T) {
	for _, raw := range []string{
		"Description\x00",               // missing the compression flag and method bytes
		"just some bytes with no null",  // no NUL at all
		"Description\x00\x00\x00en",     // flags + language, but no separators for translated/text
		"Description\x00\x00\x00en\x00", // translated keyword has no terminator
	} {
		r := ParseRecord([]byte(raw), 7)
		if !r.Unparseable || r.Seq != 7 || r.Keyword != "" || r.Text != "" {
			t.Errorf("%q: got %+v, want unparseable", raw, r)
		}
	}
}

func TestAcceptsAnEmptyKeyword(t *testing.T) {
	r := ParseRecord(itxt("", itxtOpts{text: "some text"}), 0)

	if r.Unparseable || r.Keyword != "" || r.Text != "some text" {
		t.Fatalf("got %+v", r)
	}
}

func TestParsesRealisticJSONPayload(t *testing.T) {
	const json = `{"author":{"id":"usr_abc","displayName":"Test"}}`
	r := ParseRecord(itxt("Description", itxtOpts{text: json}), 0)

	if r.Text != json {
		t.Fatalf("got %q", r.Text)
	}
}

func TestDecodesInvalidUTF8WithReplacementCharacters(t *testing.T) {
	// Matches Python's errors="replace" rather than failing.
	r := ParseRecord([]byte{'K', 0, 0, 0, 0, 0, 0xFF, 0xFE}, 0)

	if r.Keyword != "K" || r.Text != "\ufffd\ufffd" {
		t.Fatalf("got %+v", r)
	}
}

func TestDecodeReplacesMaximalSubparts(t *testing.T) {
	// One U+FFFD per maximal subpart, as Python and .NET do; Go's own helpers
	// would give a different count for most of these.
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("plain"), "plain"},
		{[]byte("Night\xc3\xa2\xcb\x86\xe2\x80\x99"), "Night\u00e2\u02c6\u2019"}, // valid mojibake is kept
		{[]byte{0xE2, 0x82}, "\ufffd"},                                           // truncated 3-byte sequence
		{[]byte{0xE2, 0x82, 'a'}, "\ufffda"},                                     // truncated, then ASCII
		{[]byte{0xC0, 0xAF}, "\ufffd\ufffd"},                                     // overlong lead is never valid
		{[]byte{0xED, 0xA0, 0x80}, "\ufffd\ufffd\ufffd"},                         // surrogate
		{[]byte{0xF0, 0x9F, 0x98}, "\ufffd"},                                     // truncated 4-byte
		{[]byte{0xF0, 0x9F, 0x98, 0x80}, "\U0001F600"},
		{[]byte{0x80, 0x80}, "\ufffd\ufffd"},
		{[]byte{0xF5, 0x80}, "\ufffd\ufffd"},
	}
	for _, c := range cases {
		if got := DecodeUTF8(c.in); got != c.want {
			t.Errorf("DecodeUTF8(% x) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- stream level ----

func TestReadsMultipleItxtChunksInOrderAndSkipsOthers(t *testing.T) {
	data := buildPNG(
		chunk{"IHDR", make([]byte, 13)},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "first"})},
		chunk{"IDAT", make([]byte, 64)},
		chunk{"iTXt", itxt("XML:com.adobe.xmp", itxtOpts{text: "<x/>"})},
		chunk{"IEND", nil},
	)

	res := Read(bytes.NewReader(data))

	if res.IOError || res.Err != nil {
		t.Fatalf("unexpected error: %+v", res)
	}
	if len(res.Records) != 2 {
		t.Fatalf("got %d records", len(res.Records))
	}
	if res.Records[0].Seq != 0 || res.Records[0].Text != "first" {
		t.Errorf("record 0: %+v", res.Records[0])
	}
	if res.Records[1].Seq != 1 || res.Records[1].Keyword != "XML:com.adobe.xmp" {
		t.Errorf("record 1: %+v", res.Records[1])
	}
}

func TestSkipsChunksOnANonSeekableStream(t *testing.T) {
	data := buildPNG(
		chunk{"IDAT", make([]byte, 10000)},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "after image"})},
	)

	res := Read(io.MultiReader(bytes.NewReader(data))) // hides Seek

	if res.Err != nil || len(res.Records) != 1 || res.Records[0].Text != "after image" {
		t.Fatalf("got %+v", res)
	}
}

func TestStopsAtIEND(t *testing.T) {
	data := buildPNG(
		chunk{"iTXt", itxt("Description", itxtOpts{text: "kept"})},
		chunk{"IEND", nil},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "after the end"})},
	)

	res := Read(bytes.NewReader(data))

	if len(res.Records) != 1 || res.Records[0].Text != "kept" {
		t.Fatalf("got %+v", res.Records)
	}
}

func TestRejectsNonPNGData(t *testing.T) {
	res := Read(bytes.NewReader([]byte("this is not a png")))

	if len(res.Records) != 0 || res.IOError || !errors.Is(res.Err, ErrNotPNG) {
		t.Fatalf("got %+v", res)
	}
}

func TestRejectsATruncatedSignature(t *testing.T) {
	res := Read(bytes.NewReader([]byte{0x89, 0x50}))

	if res.IOError || !errors.Is(res.Err, ErrNotPNG) {
		t.Fatalf("got %+v", res)
	}
}

func TestReturnsWhatItHasWhenTheFileIsTruncatedMidStream(t *testing.T) {
	data := buildPNG(chunk{"iTXt", itxt("Description", itxtOpts{text: "complete"})})
	// Chop off part of the trailing CRC; the next read hits the end.
	truncated := data[:len(data)-2]

	res := Read(bytes.NewReader(truncated))

	if res.Err != nil || len(res.Records) != 1 || res.Records[0].Text != "complete" {
		t.Fatalf("got %+v", res)
	}
}

func TestReportsAnIOErrorForAMissingFile(t *testing.T) {
	res := ReadFile(filepath.Join(t.TempDir(), "definitely-not-here.png"))

	if !res.IOError || res.Err == nil || len(res.Records) != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestRefusesAnAbsurdChunkLength(t *testing.T) {
	// A corrupt length field must not become a huge allocation, and must not
	// cost the iTXt chunks read before it.
	data := buildPNG(chunk{"iTXt", itxt("Description", itxtOpts{text: "before corruption"})})
	data = append(data, 0x7F, 0xFF, 0xFF, 0xFF)
	data = append(data, "iTXt"...)

	res := Read(bytes.NewReader(data))

	if res.IOError || !errors.Is(res.Err, ErrChunkTooLarge) {
		t.Fatalf("got %+v", res)
	}
	if len(res.Records) != 1 || res.Records[0].Text != "before corruption" {
		t.Fatalf("records: %+v", res.Records)
	}
}

// failingReader serves data, then fails with a non-EOF error.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestAReadErrorIsAnIOErrorAndKeepsRecordsReadSoFar(t *testing.T) {
	// A share dropping mid-file is environmental: the caller should retry, and
	// may log what was read.
	boom := errors.New("network name no longer available")
	data := buildPNG(chunk{"iTXt", itxt("Description", itxtOpts{text: "before failure"})})

	res := Read(&failingReader{data: data, err: boom})

	if !res.IOError || !errors.Is(res.Err, boom) {
		t.Fatalf("got %+v", res)
	}
	if len(res.Records) != 1 || res.Records[0].Text != "before failure" {
		t.Fatalf("records: %+v", res.Records)
	}
}

func TestReadFileSeeksPastLargeChunks(t *testing.T) {
	// Exercises the buffered-file skip path both within and beyond the buffer.
	data := buildPNG(
		chunk{"IHDR", make([]byte, 13)},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "one"})},
		chunk{"IDAT", bytes.Repeat([]byte{0xAB}, 100*1024)},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "two"})},
		chunk{"IDAT", make([]byte, 10)},
		chunk{"iTXt", itxt("Description", itxtOpts{text: "three"})},
		chunk{"IEND", nil},
	)
	path := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	res := ReadFile(path)

	if res.Err != nil || len(res.Records) != 3 {
		t.Fatalf("got %+v", res)
	}
	for i, want := range []string{"one", "two", "three"} {
		if res.Records[i].Text != want || res.Records[i].Seq != i {
			t.Errorf("record %d: %+v", i, res.Records[i])
		}
	}
}
