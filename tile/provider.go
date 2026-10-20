// Package tile fetches, caches, and places raster map tiles from XYZ tile
// servers such as OpenStreetMap. It owns the Web Mercator math, the HTTP
// fetching with an identifying User-Agent, an in-memory LRU plus an
// optional on-disk cache, and the bookkeeping that lets several renders
// share one in-flight download. The root ntmaps package composes the
// tiles it returns into the terminal cell rectangle.
package tile

import (
	"image/color"
	"strconv"
	"strings"
)

// Provider describes one XYZ tile service.
//
// URL is a template with {z}, {x}, {y}, and optionally {s} for a shard
// chosen round-robin from Shards. An empty URL is a valid "no tiles"
// provider: renders fill the view with Placeholder and fetch nothing.
//
// Attribution is drawn onto every rendered map. Each service publishes
// its own terms; read them before shipping a public app.
type Provider struct {
	Name        string
	URL         string
	Attribution string
	Shards      []string

	// TileSize is the edge length of one tile in pixels. Zero means 256.
	TileSize int

	// MaxZoom is the highest zoom the service serves. Zero means 19.
	MaxZoom int

	// Placeholder fills tiles that have not arrived yet and areas outside
	// the world. nil means a light gray that suits most street styles;
	// dark styles should set a dark color so loading does not flash.
	Placeholder color.Color
}

// DefaultPlaceholder is the fill used when Provider.Placeholder is nil.
var DefaultPlaceholder color.Color = color.RGBA{R: 0xe6, G: 0xe6, B: 0xe6, A: 0xff}

const (
	defaultTileSize = 256
	defaultMaxZoom  = 19
)

// Valid reports whether the provider can serve tiles at all.
func (p Provider) Valid() bool { return p.URL != "" }

// Size returns TileSize with the default applied.
func (p Provider) Size() int {
	if p.TileSize <= 0 {
		return defaultTileSize
	}
	return p.TileSize
}

// Max returns MaxZoom with the default applied.
func (p Provider) Max() int {
	if p.MaxZoom <= 0 {
		return defaultMaxZoom
	}
	return p.MaxZoom
}

// Fill returns Placeholder with the default applied.
func (p Provider) Fill() color.Color {
	if p.Placeholder == nil {
		return DefaultPlaceholder
	}
	return p.Placeholder
}

// TileURL expands the URL template for one tile. The shard, when the
// template uses {s}, is Shards[(x+y) mod len(Shards)] so neighboring tiles
// spread across hosts.
func (p Provider) TileURL(z, x, y int) string {
	if !p.Valid() {
		return ""
	}
	r := strings.NewReplacer(
		"{z}", strconv.Itoa(z),
		"{x}", strconv.Itoa(x),
		"{y}", strconv.Itoa(y),
		"{s}", p.shard(x, y),
	)
	return r.Replace(p.URL)
}

func (p Provider) shard(x, y int) string {
	if len(p.Shards) == 0 {
		return ""
	}
	i := (x + y) % len(p.Shards)
	if i < 0 {
		i += len(p.Shards)
	}
	return p.Shards[i]
}

var darkPlaceholder color.Color = color.RGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}

// OpenStreetMap is the OpenStreetMap Foundation's standard style.
// See https://operations.osmfoundation.org/policies/tiles/ before using it
// in anything heavier than an interactive viewer.
func OpenStreetMap() Provider {
	return Provider{
		Name:        "osm",
		URL:         "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
		Shards:      []string{"a", "b", "c"},
		Attribution: "Maps and Data (c) openstreetmap.org and contributors, ODbL",
		MaxZoom:     19,
	}
}

// Wikimedia is the Wikimedia Foundation's OSM-based international style.
func Wikimedia() Provider {
	return Provider{
		Name:        "wikimedia",
		URL:         "https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png",
		Attribution: "Map (c) Wikimedia; Data (c) OSM and contributors, ODbL.",
		MaxZoom:     19,
	}
}

// OpenTopoMap renders OSM data with SRTM relief.
func OpenTopoMap() Provider {
	return Provider{
		Name:        "opentopomap",
		URL:         "https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png",
		Shards:      []string{"a", "b", "c"},
		Attribution: "Maps (c) OpenTopoMap [CC-BY-SA]; Data (c) OSM and contributors [ODbL]; Data (c) SRTM",
		MaxZoom:     17,
	}
}

func carto(name, style string) Provider {
	return Provider{
		Name:        "carto-" + name,
		URL:         "https://cartodb-basemaps-{s}.global.ssl.fastly.net/" + style + "/{z}/{x}/{y}.png",
		Shards:      []string{"a", "b", "c", "d"},
		Attribution: "Map (c) Carto [CC BY 3.0] Data (c) OSM and contributors, ODbL.",
		MaxZoom:     19,
	}
}

// CartoLight is Carto's Positron basemap.
func CartoLight() Provider { return carto("light", "light_all") }

// CartoDark is Carto's Dark Matter basemap.
func CartoDark() Provider {
	p := carto("dark", "dark_all")
	p.Placeholder = darkPlaceholder
	return p
}

// ArcGISWorldImagery is Esri's satellite and aerial imagery layer.
func ArcGISWorldImagery() Provider {
	return Provider{
		Name:        "arcgis-worldimagery",
		URL:         "https://server.arcgisonline.com/arcgis/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
		Attribution: "Source: Esri, Maxar, GeoEye, Earthstar Geographics, CNES/Airbus DS, USDA, USGS, AeroGRID, IGN, and the GIS User Community",
		MaxZoom:     19,
		Placeholder: darkPlaceholder,
	}
}
