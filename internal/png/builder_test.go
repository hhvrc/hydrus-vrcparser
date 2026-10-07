package png

import (
	"bytes"
	"encoding/binary"
)

// itxtOpts are the optional fields of a test iTXt payload.
type itxtOpts struct {
	compFlag, compMethod byte
	lang, trans, text    string
}

// itxt builds an iTXt chunk payload:
// keyword \0 compFlag compMethod language \0 translated \0 text.
func itxt(keyword string, o itxtOpts) []byte {
	var b bytes.Buffer
	b.WriteString(keyword)
	b.WriteByte(0)
	b.WriteByte(o.compFlag)
	b.WriteByte(o.compMethod)
	b.WriteString(o.lang)
	b.WriteByte(0)
	b.WriteString(o.trans)
	b.WriteByte(0)
	b.WriteString(o.text)
	return b.Bytes()
}

type chunk struct {
	typ  string
	data []byte
}

// buildPNG builds minimal PNG bytes. CRCs are zero; nothing verifies them.
func buildPNG(chunks ...chunk) []byte {
	b := bytes.NewBuffer(append([]byte(nil), signature...))
	var length [4]byte
	for _, c := range chunks {
		binary.BigEndian.PutUint32(length[:], uint32(len(c.data)))
		b.Write(length[:])
		b.WriteString(c.typ)
		b.Write(c.data)
		b.Write(make([]byte, 4))
	}
	return b.Bytes()
}
