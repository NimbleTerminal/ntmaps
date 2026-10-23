// Package geodata reads the files people keep of things on a map, GeoJSON
// and GPX, into what a map draws: places as markers, tracks, routes and the
// outlines of areas as paths, and the box around them all. Reading is
// bounded by Limits, as for a file from anywhere.
package geodata

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"nimbleterminal.dev/ntmaps"
)

// Feature is one named thing in a file: a GeoJSON feature or bare geometry,
// or a GPX waypoint, route or track. It may hold several parts, as a
// MultiLineString or a track of several segments does.
type Feature struct {
	// Name is the GeoJSON "name" (else "title") property, or the GPX
	// <name>; empty when there is none.
	Name string

	Places []ntmaps.Point     // points, each drawn as a marker
	Lines  [][]ntmaps.Point   // lines: tracks, routes, LineStrings
	Areas  [][][]ntmaps.Point // polygons: the outer ring, then any holes, each closed
}

// Data is what a file holds.
type Data struct {
	Format   string // "geojson" or "gpx"
	Features []Feature
	Points   int           // positions read, across every feature
	Bounds   ntmaps.Bounds // the box around every position; zero when Points is 0
}

// Markers are the places of every feature, in the map's default style.
func (d Data) Markers() []ntmaps.Marker {
	var out []ntmaps.Marker
	for _, f := range d.Features {
		for _, p := range f.Places {
			out = append(out, ntmaps.Marker{Lat: p.Lat, Lng: p.Lng})
		}
	}
	return out
}

// areaWidth draws the outline of an area thinner than a line, so a track
// along a boundary stays visible.
const areaWidth = 2

// Paths are the lines of every feature in the map's default style, and the
// rings of its areas as thinner outlines.
func (d Data) Paths() []ntmaps.Path {
	var out []ntmaps.Path
	for _, f := range d.Features {
		for _, l := range f.Lines {
			out = append(out, ntmaps.Path{Points: l})
		}
		for _, a := range f.Areas {
			for _, ring := range a {
				out = append(out, ntmaps.Path{Points: ring, Width: areaWidth})
			}
		}
	}
	return out
}

// Limits bound a read. A zero field takes its DefaultLimits value.
type Limits struct {
	Bytes    int64 // the most of the file read
	Points   int   // positions, across every feature
	Features int
}

// DefaultLimits suit a file opened by hand: a long day's GPS track at one
// point a second is under 100,000 positions.
var DefaultLimits = Limits{Bytes: 64 << 20, Points: 1_000_000, Features: 100_000}

func (l Limits) orDefault() Limits {
	if l.Bytes <= 0 {
		l.Bytes = DefaultLimits.Bytes
	}
	if l.Points <= 0 {
		l.Points = DefaultLimits.Points
	}
	if l.Features <= 0 {
		l.Features = DefaultLimits.Features
	}
	return l
}

// ErrLimit is wrapped by the error of a file over one of its Limits.
var ErrLimit = errors.New("over the limit")

// maxDepth bounds nesting: GeoJSON geometry collections, GPX elements.
const maxDepth = 64

// Read reads GeoJSON or GPX, telling them apart by their first character.
func Read(r io.Reader, l Limits) (Data, error) {
	br := bufio.NewReader(r)
	for {
		c, _, err := br.ReadRune()
		if err != nil {
			if err == io.EOF {
				return Data{}, errors.New("geodata: empty file")
			}
			return Data{}, err
		}
		switch c {
		case ' ', '\t', '\r', '\n', '\ufeff':
			continue
		case '{':
			_ = br.UnreadRune()
			return ReadGeoJSON(br, l)
		case '<':
			_ = br.UnreadRune()
			return ReadGPX(br, l)
		}
		return Data{}, errors.New("geodata: neither GeoJSON nor GPX")
	}
}

// readAll reads the whole file, within the byte limit.
func readAll(r io.Reader, l Limits) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, l.Bytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > l.Bytes {
		return nil, fmt.Errorf("geodata: file over %d bytes: %w", l.Bytes, ErrLimit)
	}
	return bytes.TrimPrefix(data, []byte("\ufeff")), nil
}

// builder collects features within the limits and grows the bounds.
type builder struct {
	l    Limits
	data Data
}

func (b *builder) feature(f Feature) error {
	if len(f.Places)+len(f.Lines)+len(f.Areas) == 0 {
		return nil
	}
	if len(b.data.Features) == b.l.Features {
		return fmt.Errorf("geodata: more than %d features: %w", b.l.Features, ErrLimit)
	}
	b.data.Features = append(b.data.Features, f)
	return nil
}

// point checks a position and counts it into the bounds.
func (b *builder) point(lat, lng float64) (ntmaps.Point, error) {
	if math.IsNaN(lat) || math.IsNaN(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return ntmaps.Point{}, fmt.Errorf("geodata: position %g, %g is not a latitude and longitude", lat, lng)
	}
	if b.data.Points == b.l.Points {
		return ntmaps.Point{}, fmt.Errorf("geodata: more than %d positions: %w", b.l.Points, ErrLimit)
	}
	if b.data.Points == 0 {
		b.data.Bounds = ntmaps.Bounds{South: lat, North: lat, West: lng, East: lng}
	} else {
		b.data.Bounds = b.data.Bounds.Extend(lat, lng)
	}
	b.data.Points++
	return ntmaps.Point{Lat: lat, Lng: lng}, nil
}
