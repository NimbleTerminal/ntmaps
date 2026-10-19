// Package city is experimental scaffolding for geographic building scenes.
// It has no terminal or renderer dependencies. Its API is intentionally internal.
package city

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const earthRadius = 6378137.0

// Coordinate uses degrees. Scenes use a local east/north/up frame in meters.
type Coordinate struct{ Lat, Lon float64 }

func (p Coordinate) Valid() bool {
	return finite(p.Lat) && finite(p.Lon) && math.Abs(p.Lat) <= 85 && math.Abs(p.Lon) <= 180
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

type Bounds struct{ South, West, North, East float64 }

func (b Bounds) Validate() error {
	if !(Coordinate{b.South, b.West}).Valid() || !(Coordinate{b.North, b.East}).Valid() || b.North <= b.South || b.East <= b.West {
		return fmt.Errorf("city: invalid bounds (antimeridian bounds are unsupported)")
	}
	return nil
}
func (b Bounds) Center() Coordinate {
	return Coordinate{(b.South + b.North) / 2, (b.West + b.East) / 2}
}
func Around(center Coordinate, radius float64) (Bounds, error) {
	if !center.Valid() || !finite(radius) || radius < 25 || radius > 500 {
		return Bounds{}, fmt.Errorf("city: radius must be 25..500 meters at a valid latitude/longitude")
	}
	lat := radius / earthRadius * 180 / math.Pi
	lon := lat / math.Cos(center.Lat*math.Pi/180)
	b := Bounds{center.Lat - lat, center.Lon - lon, center.Lat + lat, center.Lon + lon}
	return b, b.Validate()
}

// Projection applies Web Mercator locally, corrected to ground meters at Origin.
// It is suitable for neighborhood extents; elevations are relative to flat ground.
type Projection struct{ Origin Coordinate }

func (p Projection) Project(c Coordinate) (x, y float64) {
	scale := math.Cos(p.Origin.Lat * math.Pi / 180)
	x = earthRadius * (c.Lon - p.Origin.Lon) * math.Pi / 180 * scale
	merc := func(lat float64) float64 { return math.Log(math.Tan(math.Pi/4 + lat*math.Pi/360)) }
	y = earthRadius * (merc(c.Lat) - merc(p.Origin.Lat)) * scale
	return
}

type HeightSource string

const (
	HeightTag     HeightSource = "height"
	HeightLevels  HeightSource = "levels"
	HeightDefault HeightSource = "default"
)

type Height struct {
	Top, Base float64
	Source    HeightSource
}
type HeightPolicy struct{ MetersPerLevel, DefaultHeight float64 }

func DefaultHeightPolicy() HeightPolicy { return HeightPolicy{3, 9} }
func (p HeightPolicy) Validate() error {
	if !finite(p.MetersPerLevel) || p.MetersPerLevel <= 0 || p.MetersPerLevel > 10 || !finite(p.DefaultHeight) || p.DefaultHeight <= 0 || p.DefaultHeight > 1000 {
		return fmt.Errorf("city: invalid height policy")
	}
	return nil
}
func meters(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	factor := 1.0
	for _, u := range []string{"meters", "metres", "meter", "metre", "m", "feet", "foot", "ft", "'"} {
		if strings.HasSuffix(s, u) {
			if u == "feet" || u == "foot" || u == "ft" || u == "'" {
				factor = .3048
			}
			s = strings.TrimSpace(strings.TrimSuffix(s, u))
			break
		}
	}
	v, e := strconv.ParseFloat(s, 64)
	v *= factor
	return v, e == nil && finite(v) && v >= 0 && v <= 1000
}
func positive(s string) (float64, bool) {
	v, e := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, e == nil && finite(v) && v > 0 && v <= 200
}

// Resolve prefers height (which includes the roof), then floors plus roof height,
// then the policy default. Malformed tags never poison the scene with NaN/Inf.
func (p HeightPolicy) Resolve(tags map[string]string) Height {
	h := Height{Top: p.DefaultHeight, Source: HeightDefault}
	if v, ok := meters(tags["height"]); ok && v > 0 {
		h.Top, h.Source = v, HeightTag
	} else if n, ok := positive(tags["building:levels"]); ok {
		h.Top, h.Source = n*p.MetersPerLevel, HeightLevels
		if roof, ok := meters(tags["roof:height"]); ok {
			h.Top += roof
		} else if n, ok := positive(tags["roof:levels"]); ok {
			h.Top += n * p.MetersPerLevel
		}
	}
	if base, ok := meters(tags["min_height"]); ok {
		h.Base = base
	} else if n, ok := positive(tags["building:min_level"]); ok {
		h.Base = n * p.MetersPerLevel
	}
	if h.Base >= h.Top {
		h.Base = 0
	} // Retain a valid visible volume for inconsistent tags.
	return h
}

type Building struct {
	ID        string
	Footprint []Coordinate
	Tags      map[string]string
	Part      bool
}
type Road struct{ Points []Coordinate }
type Dataset struct {
	Name, Attribution             string
	Bounds                        Bounds
	Buildings                     []Building
	Roads                         []Road
	SkippedRelations, SkippedWays int
}

// Unproject is the inverse of Project for neighborhood-scale navigation.
func (p Projection) Unproject(x, y float64) Coordinate {
	scale := math.Cos(p.Origin.Lat * math.Pi / 180)
	lon := p.Origin.Lon + x/(earthRadius*scale)*180/math.Pi
	originY := math.Log(math.Tan(math.Pi/4 + p.Origin.Lat*math.Pi/360))
	lat := (2*math.Atan(math.Exp(originY+y/(earthRadius*scale))) - math.Pi/2) * 180 / math.Pi
	return Coordinate{lat, lon}
}
