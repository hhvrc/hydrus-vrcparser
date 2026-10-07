// Package png extracts iTXt chunks from PNG files.
//
// Content-type classification is a tagger concern and lives with the VRChat
// tagger, not here.
package png

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Record is one iTXt chunk. Seq is its position among the file's iTXt chunks,
// counting unparseable ones.
type Record struct {
	Seq               int
	Keyword           string
	CompressionFlag   int
	CompressionMethod int
	LanguageTag       string
	TranslatedKeyword string
	Text              string

	// Unparseable marks a chunk whose payload lacked the separators the iTXt
	// layout requires. Every other field except Seq is then zero.
	Unparseable bool
}

// Result is the outcome of reading one file.
//
// Records holds every iTXt chunk read before reading stopped, so it can be
// non-empty alongside an error. IOError distinguishes environmental failures
// (missing file, disconnected share, read error), which callers should retry,
// from format errors (Err set, IOError false), which will not get better.
type Result struct {
	Records []Record
	IOError bool
	Err     error
}

var (
	// ErrNotPNG is reported when the signature is missing or wrong.
	ErrNotPNG = errors.New("not a valid PNG file")

	// ErrChunkTooLarge is reported when an iTXt length field exceeds
	// MaxChunkLength; the message carries the length.
	ErrChunkTooLarge = errors.New("iTXt chunk length exceeds sanity limit")
)

// MaxChunkLength guards against a corrupt length field asking us to allocate
// wildly. No legitimate iTXt chunk approaches this; the PNG spec caps chunks at
// 2^31-1.
const MaxChunkLength = 64 * 1024 * 1024

var signature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// ReadFile reads the iTXt chunks of the PNG at path.
func ReadFile(path string) Result {
	f, err := os.Open(path)
	if err != nil {
		// Transient or environmental: the legacy pipeline retried these rather
		// than marking the file failed.
		return Result{IOError: true, Err: err}
	}
	defer f.Close()

	// Buffered for the many small header reads, but able to seek past image
	// data instead of reading it -- files often live on a network share.
	return read(&fileReader{f: f, br: bufio.NewReaderSize(f, 32*1024)})
}

// Read reads the iTXt chunks of a PNG stream. Non-iTXt chunks are skipped by
// seeking when r is an io.Seeker, and by discarding otherwise.
func Read(r io.Reader) Result {
	return read(&plainReader{r: r})
}

// skipReader is a reader that can also skip forward cheaply.
type skipReader interface {
	io.Reader
	// skip advances n bytes. Running off the end is not an error: the next
	// read reports EOF, which ends the scan as a truncated file.
	skip(n int64) error
}

func read(r skipReader) Result {
	var records []Record

	sig := make([]byte, len(signature))
	if ok, err := readExactly(r, sig); err != nil {
		return Result{IOError: true, Err: err}
	} else if !ok || !bytes.Equal(sig, signature) {
		return Result{Err: ErrNotPNG}
	}

	header := make([]byte, 8)
	seq := 0
	for {
		ok, err := readExactly(r, header)
		if err != nil {
			return Result{Records: records, IOError: true, Err: err}
		}
		if !ok {
			// Truncated: keep what we have, as the legacy reader did.
			break
		}

		size := binary.BigEndian.Uint32(header[:4])
		typ := string(header[4:8])

		if typ == "IEND" {
			break
		}

		if typ != "iTXt" {
			// Fast path: never pull image data into memory.
			if err := r.skip(int64(size) + 4); err != nil {
				return Result{Records: records, IOError: true, Err: err}
			}
			continue
		}

		if size > MaxChunkLength {
			// Format error, not I/O: a corrupt file stays corrupt, so the
			// caller should record it rather than retry it every run.
			return Result{Records: records, Err: fmt.Errorf("%w: length %d", ErrChunkTooLarge, size)}
		}

		data := make([]byte, size)
		ok, err = readExactly(r, data)
		if err != nil {
			return Result{Records: records, IOError: true, Err: err}
		}
		if !ok {
			break
		}

		if err := r.skip(4); err != nil { // CRC; nothing verifies it.
			return Result{Records: records, IOError: true, Err: err}
		}
		records = append(records, ParseRecord(data, seq))
		seq++
	}

	return Result{Records: records}
}

// ParseRecord parses an iTXt payload:
//
//	keyword \0 comp_flag(1) comp_method(1) language_tag \0 translated_keyword \0 text
//
// The two flag bytes are raw bytes, not NUL-terminated strings, so they are
// read positionally rather than by splitting on NUL. Only the first two NULs
// after them are separators; any later ones belong to the text.
//
// Compressed iTXt (compression flag 1) is deliberately not inflated: the
// legacy parser did not either, no VRChat writer produces it, and inflating now
// would change what is stored for any such chunk already in the database. Such
// a chunk yields its raw bytes as text, with its flag preserved so a consumer
// can tell.
func ParseRecord(data []byte, seq int) Record {
	nul := bytes.IndexByte(data, 0)
	if nul < 0 || nul+2 >= len(data) {
		return Record{Seq: seq, Unparseable: true}
	}

	keyword := DecodeUTF8(data[:nul])
	compressionFlag := int(data[nul+1])
	compressionMethod := int(data[nul+2])
	remainder := data[nul+3:]

	first := bytes.IndexByte(remainder, 0)
	if first < 0 {
		return Record{Seq: seq, Unparseable: true}
	}
	afterLang := remainder[first+1:]
	second := bytes.IndexByte(afterLang, 0)
	if second < 0 {
		return Record{Seq: seq, Unparseable: true}
	}

	return Record{
		Seq:               seq,
		Keyword:           keyword,
		CompressionFlag:   compressionFlag,
		CompressionMethod: compressionMethod,
		LanguageTag:       DecodeUTF8(remainder[:first]),
		TranslatedKeyword: DecodeUTF8(afterLang[:second]),
		Text:              DecodeUTF8(afterLang[second+1:]),
	}
}

// readExactly fills buf. It reports false, nil when the stream ends first, and
// an error only for a genuine read failure.
func readExactly(r io.Reader, buf []byte) (bool, error) {
	_, err := io.ReadFull(r, buf)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return false, nil
	default:
		return false, err
	}
}

// fileReader buffers an *os.File while still letting large skips seek.
type fileReader struct {
	f  *os.File
	br *bufio.Reader
}

func (r *fileReader) Read(p []byte) (int, error) { return r.br.Read(p) }

func (r *fileReader) skip(n int64) error {
	buffered := int64(r.br.Buffered())
	if n <= buffered {
		_, err := r.br.Discard(int(n))
		return err
	}
	// The file position is ahead of the logical position by what is still
	// buffered; drop the buffer and seek the rest.
	if _, err := r.br.Discard(int(buffered)); err != nil {
		return err
	}
	if _, err := r.f.Seek(n-buffered, io.SeekCurrent); err != nil {
		return err
	}
	r.br.Reset(r.f)
	return nil
}

// plainReader adapts any io.Reader.
type plainReader struct {
	r io.Reader
}

func (r *plainReader) Read(p []byte) (int, error) { return r.r.Read(p) }

func (r *plainReader) skip(n int64) error {
	if s, ok := r.r.(io.Seeker); ok {
		_, err := s.Seek(n, io.SeekCurrent)
		return err
	}
	_, err := io.CopyN(io.Discard, r.r, n)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
