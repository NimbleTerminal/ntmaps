package geodata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"nimbleterminal.dev/ntmaps"
)

// object is any GeoJSON object: a feature collection, a feature, or a
// geometry. Only the members a map needs are kept.
type object struct {
	Type        string                     `json:"type"`
	Features    []json.RawMessage          `json:"features"`
	Geometry    json.RawMessage            `json:"geometry"`
	Properties  map[string]json.RawMessage `json:"properties"`
	Geometries  []json.RawMessage          `json:"geometries"`
	Coordinates json.RawMessage            `json:"coordinates"`
}

// ReadGeoJSON reads a GeoJSON (RFC 7946) feature collection, feature or
// geometry. A feature without a geometry is skipped, as the format allows;
// anything else malformed is an error.
func ReadGeoJSON(r io.Reader, l Limits) (Data, error) {
	l = l.orDefault()
	raw, err := readAll(r, l)
	if err != nil {
		return Data{}, err
	}
	b := &builder{l: l, data: Data{Format: "geojson"}}
	var top object
	if err := json.Unmarshal(raw, &top); err != nil {
		return Data{}, fmt.Errorf("geodata: GeoJSON: %w", err)
	}
	switch top.Type {
	case "FeatureCollection":
		for _, raw := range top.Features {
			var o object
			if err := json.Unmarshal(raw, &o); err != nil {
				return Data{}, fmt.Errorf("geodata: GeoJSON: %w", err)
			}
			if o.Type != "Feature" {
				return Data{}, fmt.Errorf("geodata: GeoJSON: a %q among the features", o.Type)
			}
			if err := b.geoFeature(o); err != nil {
				return Data{}, err
			}
		}
	case "Feature":
		if err := b.geoFeature(top); err != nil {
			return Data{}, err
		}
	case "":
		return Data{}, errors.New("geodata: GeoJSON: no type")
	default:
		var f Feature
		if err := b.geometry(&f, top, 0); err != nil {
			return Data{}, err
		}
		if err := b.feature(f); err != nil {
			return Data{}, err
		}
	}
	return b.data, nil
}

func (b *builder) geoFeature(o object) error {
	f := Feature{Name: name(o.Properties)}
	if len(o.Geometry) == 0 || string(o.Geometry) == "null" {
		return nil
	}
	var g object
	if err := json.Unmarshal(o.Geometry, &g); err != nil {
		return fmt.Errorf("geodata: GeoJSON: %w", err)
	}
	if err := b.geometry(&f, g, 0); err != nil {
		return err
	}
	return b.feature(f)
}

// name is a feature's "name" property, else its "title", when a string.
func name(props map[string]json.RawMessage) string {
	for _, key := range []string{"name", "title"} {
		var s string
		if json.Unmarshal(props[key], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// geometry adds a geometry's parts to the feature.
func (b *builder) geometry(f *Feature, g object, depth int) error {
	if g.Type == "GeometryCollection" {
		if depth == maxDepth {
			return fmt.Errorf("geodata: GeoJSON: geometry collections nested over %d deep: %w", maxDepth, ErrLimit)
		}
		for _, raw := range g.Geometries {
			var o object
			if err := json.Unmarshal(raw, &o); err != nil {
				return fmt.Errorf("geodata: GeoJSON: %w", err)
			}
			if err := b.geometry(f, o, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	switch g.Type {
	case "Point":
		var c []float64
		if err = coordinates(g, &c); err == nil {
			var p ntmaps.Point
			if p, err = b.position(c); err == nil {
				f.Places = append(f.Places, p)
			}
		}
	case "MultiPoint":
		var c [][]float64
		if err = coordinates(g, &c); err == nil {
			var ps []ntmaps.Point
			if ps, err = b.positions(c); err == nil {
				f.Places = append(f.Places, ps...)
			}
		}
	case "LineString":
		var c [][]float64
		if err = coordinates(g, &c); err == nil {
			err = b.line(f, c)
		}
	case "MultiLineString":
		var c [][][]float64
		if err = coordinates(g, &c); err == nil {
			for _, l := range c {
				if err = b.line(f, l); err != nil {
					break
				}
			}
		}
	case "Polygon":
		var c [][][]float64
		if err = coordinates(g, &c); err == nil {
			err = b.polygon(f, c)
		}
	case "MultiPolygon":
		var c [][][][]float64
		if err = coordinates(g, &c); err == nil {
			for _, p := range c {
				if err = b.polygon(f, p); err != nil {
					break
				}
			}
		}
	default:
		return fmt.Errorf("geodata: GeoJSON: unknown geometry %q", g.Type)
	}
	return err
}

func coordinates(g object, dst any) error {
	if err := json.Unmarshal(g.Coordinates, dst); err != nil {
		return fmt.Errorf("geodata: GeoJSON: %s coordinates: %w", g.Type, err)
	}
	return nil
}

// position is a GeoJSON position: longitude, latitude, then any altitude.
func (b *builder) position(c []float64) (ntmaps.Point, error) {
	if len(c) < 2 {
		return ntmaps.Point{}, errors.New("geodata: GeoJSON: a position needs a longitude and a latitude")
	}
	return b.point(c[1], c[0])
}

func (b *builder) positions(c [][]float64) ([]ntmaps.Point, error) {
	out := make([]ntmaps.Point, 0, len(c))
	for _, p := range c {
		pt, err := b.position(p)
		if err != nil {
			return nil, err
		}
		out = append(out, pt)
	}
	return out, nil
}

func (b *builder) line(f *Feature, c [][]float64) error {
	if len(c) < 2 {
		return errors.New("geodata: GeoJSON: a line needs two positions")
	}
	ps, err := b.positions(c)
	if err == nil {
		f.Lines = append(f.Lines, ps)
	}
	return err
}

// polygon adds the rings of a polygon, closing any left open.
func (b *builder) polygon(f *Feature, c [][][]float64) error {
	if len(c) == 0 {
		return errors.New("geodata: GeoJSON: a polygon needs a ring")
	}
	rings := make([][]ntmaps.Point, 0, len(c))
	for _, r := range c {
		if len(r) < 3 {
			return errors.New("geodata: GeoJSON: a ring needs three positions")
		}
		ps, err := b.positions(r)
		if err != nil {
			return err
		}
		if ps[0] != ps[len(ps)-1] {
			ps = append(ps, ps[0])
		}
		rings = append(rings, ps)
	}
	f.Areas = append(f.Areas, rings)
	return nil
}
