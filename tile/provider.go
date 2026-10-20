// Package tile fetches, caches, and places raster map tiles. A Source
// supplies tiles by zoom, column, and row; Provider is the Source for XYZ
// tile servers such as OpenStreetMap. Loader wraps any Source with an
// in-memory LRU, an optional on-disk cache, and the bookkeeping that lets
// several renders share one in-flight fetch. The root ntmaps package
// composes the tiles into the terminal cell rectangle.
package tile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // tile servers return PNG or JPEG
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nimbleterminal.dev/ntmaps/internal/httpua"
)

// ErrNotFound is returned when a source has no tile at the requested
// coordinates. Callers usually leave the placeholder in place.
var ErrNotFound = errors.New("tile: not found")

// ErrNoSource is returned when fetching through a Source that has no
// tiles at all, such as a Provider with an empty URL.
var ErrNoSource = errors.New("tile: source has no tiles")

// Info describes a Source: how to credit it, how big its tiles are, how
// far it zooms, and what to paint where a tile is missing.
type Info struct {
	// Name keys the tile and render caches. Distinct sources need
	// distinct names.
	Name string

	// Attribution is drawn onto every rendered map. Each service
	// publishes its own terms; read them before shipping a public app.
	Attribution string

	// TileSize is the edge length of one tile in pixels. Zero means 256.
	TileSize int

	// MaxZoom is the highest zoom the source serves. Zero means 19.
	MaxZoom int

	// Placeholder fills tiles that have not arrived yet and areas outside
	// the world. nil means a light gray that suits most street styles;
	// dark styles should set a dark color so loading does not flash.
	Placeholder color.Color
}

// DefaultPlaceholder is the fill used when Info.Placeholder is nil.
var DefaultPlaceholder color.Color = color.RGBA{R: 0xe6, G: 0xe6, B: 0xe6, A: 0xff}

const (
	defaultTileSize = 256
	defaultMaxZoom  = 19
	defaultTimeout  = 30 * time.Second
	maxTileBytes    = 8 << 20
)

// Size returns TileSize with the default applied.
func (i Info) Size() int {
	if i.TileSize <= 0 {
		return defaultTileSize
	}
	return i.TileSize
}

// Max returns MaxZoom with the default applied.
func (i Info) Max() int {
	if i.MaxZoom <= 0 {
		return defaultMaxZoom
	}
	return i.MaxZoom
}

// Fill returns Placeholder with the default applied.
func (i Info) Fill() color.Color {
	if i.Placeholder == nil {
		return DefaultPlaceholder
	}
	return i.Placeholder
}

// Source supplies map tiles. Implementations must be safe for concurrent
// use; Loader calls Tile from several goroutines at once. Return
// ErrNotFound for a tile that does not exist so renders keep the
// placeholder without reporting an error.
type Source interface {
	Info() Info
	Tile(ctx context.Context, id ID) (image.Image, error)
}

// DataSource is a Source that can also hand over a tile's encoded bytes.
// Loader caches those bytes on disk when it has a directory, which is
// cheaper than re-encoding a decoded image.
type DataSource interface {
	Source
	TileData(ctx context.Context, id ID) ([]byte, error)
}

// Fetchable reports whether s can supply tiles at all. A nil Source and
// a Provider with no URL cannot; renders paint only the placeholder for
// them and fetch nothing.
func Fetchable(s Source) bool {
	if s == nil {
		return false
	}
	if p, ok := s.(Provider); ok {
		return p.Valid()
	}
	if p, ok := s.(*Provider); ok {
		return p != nil && p.Valid()
	}
	return true
}

// Decode decodes a PNG or JPEG tile.
func Decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// Provider is a Source backed by an XYZ tile server.
//
// URL is a template with {z}, {x}, {y}, and optionally {s} for a shard
// chosen round-robin from Shards. An empty URL is a valid "no tiles"
// provider.
type Provider struct {
	Name        string
	URL         string
	Attribution string
	Shards      []string
	TileSize    int
	MaxZoom     int
	Placeholder color.Color

	// Client performs the HTTP requests. nil uses a client with a 30s
	// timeout.
	Client *http.Client

	// UserAgent identifies the application to the tile server. Tile usage
	// policies, OpenStreetMap's included, require one that names the app.
	// Empty means DefaultUserAgent. Browsers set their own, so it is not
	// sent under GOOS=js.
	UserAgent string
}

var defaultClient = &http.Client{Timeout: defaultTimeout}

// DefaultUserAgent names ntmaps and the version of the module in use.
func DefaultUserAgent() string { return httpua.Default() }

// Info implements Source.
func (p Provider) Info() Info {
	return Info{Name: p.Name, Attribution: p.Attribution, TileSize: p.TileSize, MaxZoom: p.MaxZoom, Placeholder: p.Placeholder}
}

// Valid reports whether the provider can serve tiles at all.
func (p Provider) Valid() bool { return p.URL != "" }

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

// TileData implements DataSource with one HTTP GET.
func (p Provider) TileData(ctx context.Context, id ID) ([]byte, error) {
	if !p.Valid() {
		return nil, ErrNoSource
	}
	url := p.TileURL(id.Z, id.X, id.Y)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	httpua.Set(req, p.UserAgent)
	client := p.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("tile: GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTileBytes))
}

// Tile implements Source.
func (p Provider) Tile(ctx context.Context, id ID) (image.Image, error) {
	data, err := p.TileData(ctx, id)
	if err != nil {
		return nil, err
	}
	img, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("tile: decode %d/%d/%d: %w", id.Z, id.X, id.Y, err)
	}
	return img, nil
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

// OpenSeaMap is the seamark overlay: buoys, lights, and harbour marks on
// transparent tiles, meant to be drawn over a base map.
func OpenSeaMap() Provider {
	return Provider{
		Name:        "openseamap",
		URL:         "https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png",
		Attribution: "Seamarks (c) OpenSeaMap contributors, CC BY-SA",
		MaxZoom:     18,
	}
}

// OpenRailwayMap is the railway infrastructure overlay on transparent
// tiles, meant to be drawn over a base map.
func OpenRailwayMap() Provider {
	return Provider{
		Name:        "openrailwaymap",
		URL:         "https://{s}.tiles.openrailwaymap.org/standard/{z}/{x}/{y}.png",
		Shards:      []string{"a", "b", "c"},
		Attribution: "Railways (c) OpenRailwayMap, CC BY-SA",
		MaxZoom:     19,
	}
}
