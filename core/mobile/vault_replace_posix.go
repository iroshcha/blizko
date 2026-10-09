//go:build !windows

package mobile

import "os"

func replaceVaultFile(source, destination string) error {
	return os.Rename(source, destination)
}
