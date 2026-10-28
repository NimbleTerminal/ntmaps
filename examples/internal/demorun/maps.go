package demorun

import (
	"nimbleterminal.dev/ntmaps/geocode"
	"nimbleterminal.dev/ntmaps/tile"
)

// UserAgent identifies the interactive examples to their selected services.
const UserAgent = "ntmaps-examples/0.6.0 (+https://github.com/NimbleTerminal/ntmaps)"

// OpenStreetMap opts the interactive examples into the public tile service.
// Keep map prefetch disabled with this provider.
func OpenStreetMap() tile.Provider {
	p := tile.OpenStreetMap()
	p.UserAgent = UserAgent
	return p
}

// Geocoder is shared so example lookups use one pacing state per process.
var Geocoder = &geocode.Nominatim{UserAgent: UserAgent}
