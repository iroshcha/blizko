//go:build windows

package mobile

// MoveFileEx with WRITE_THROUGH commits file replacement on Windows.
func syncVaultDirectory(string) error { return nil }
