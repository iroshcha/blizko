//go:build !iroh

package mobile

// Unit tests can exercise storage/protocol without linking native libraries.
// Mobile builds MUST use the iroh tag; this is an explicit error, never a fallback transport.
func nativeCall(string) string { return `{"error":"iroh_not_linked"}` }
