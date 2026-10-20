//go:build js

package tile

// DefaultDir returns "" because the browser has no filesystem; the
// browser's own HTTP cache stands in for the disk cache.
func DefaultDir() string { return "" }
