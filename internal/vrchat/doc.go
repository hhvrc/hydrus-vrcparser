// Package vrchat parses the metadata VRChat (and tools around it) embed in
// screenshot PNG iTXt chunks, normalizes the three source formats to one
// schema, and derives Hydrus tags from it.
//
// It began as a port of a Python tool, by way of a C# port, and was verified
// byte-for-byte against both over a real database. Both are gone; the
// reference is now the before/after parity dump (internal/parity,
// tools/parity), which every change to parsing or tag building must keep
// identical unless the change is intended. Where this package reproduces a
// quirk of the original tools, the comments say why: the tags already pushed
// to Hydrus depend on it.
//
// The three formats are:
//
//   - VRCX JSON ("json"): a JSON object with author/world/players.
//   - VRChat XMP ("xml"): an XMP packet carrying the vrc: namespace, or an
//     Adobe-resaved packet with the VRCX JSON buried in dc:description.
//   - The legacy pipe-delimited line ("line") written by screenshotmanager and
//     lfs.
//
// Inputs are Go strings and are assumed to be valid UTF-8. The original tools
// read them through a UTF-8 decoder that replaced invalid sequences with
// U+FFFD; FileTags repairs cached text the same way (png.DecodeUTF8), and any
// other caller reading raw bytes that may hold invalid UTF-8 should too. (The
// current database holds none.)
package vrchat
