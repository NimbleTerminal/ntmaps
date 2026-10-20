//go:build !js

package tile

import (
	"os"
	"path/filepath"
)

// DefaultDir returns the per-user tile cache directory,
// $XDG_CACHE_HOME/ntmaps/tiles or the platform equivalent, or "" when the
// user cache directory is unknown.
func DefaultDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "ntmaps", "tiles")
}
