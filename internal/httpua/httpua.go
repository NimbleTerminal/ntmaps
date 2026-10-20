// Package httpua holds the User-Agent that ntmaps sends to map services.
// Tile and geocoding usage policies require an identifying User-Agent.
package httpua

import "runtime/debug"

// Default names ntmaps and the version of the module in use.
func Default() string {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok {
		const module = "nimbleterminal.dev/ntmaps"
		if info.Main.Path == module && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, d := range info.Deps {
			if d.Path == module && d.Version != "" {
				version = d.Version
			}
		}
	}
	return "ntmaps/" + version + " (+https://github.com/NimbleTerminal/ntmaps)"
}
