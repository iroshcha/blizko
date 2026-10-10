//go:build !windows

package mobile

import (
	"os"
	"path/filepath"
)

func syncVaultDirectory(file string) error {
	directory, err := os.Open(filepath.Dir(file))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
