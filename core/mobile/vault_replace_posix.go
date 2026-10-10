//go:build !windows

package mobile

import "os"

func replaceVaultFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return syncVaultDirectory(destination)
}
