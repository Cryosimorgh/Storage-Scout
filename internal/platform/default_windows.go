//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

func DefaultRoot() string {
	if p := os.Getenv("USERPROFILE"); p != "" {
		return p
	}
	return filepath.VolumeName(os.Args[0]) + `\`
}