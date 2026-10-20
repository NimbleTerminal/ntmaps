package cityview

import (
	"bytes"
	_ "embed"
	"nimbleterminal.dev/ntmaps/internal/city"
)

//go:embed neighborhood.json
var neighborhood []byte

func Bundled() (city.Dataset, error) {
	d, err := city.Decode(bytes.NewReader(neighborhood), city.Bounds{South: 40.7400, West: -73.9910, North: 40.7430, East: -73.9870})
	d.Name = "Flatiron, Manhattan (bundled OSM sample)"
	return d, err
}
