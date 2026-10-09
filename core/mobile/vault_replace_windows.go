//go:build windows

package mobile

import "golang.org/x/sys/windows"

func replaceVaultFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// Replace without removing the previous encrypted state first. A failed
	// replacement must leave the last durable history intact.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
