package ntmaps

import (
	"math"

	"nimbleterminal.dev/ntmaps/tile"
)

// Bounds is a geographic rectangle. West greater than East describes a
// box that crosses the antimeridian.
type Bounds struct{ South, West, North, East float64 }

// Valid reports whether the box has a positive latitude span and lies
// within the Mercator latitude limits.
func (b Bounds) Valid() bool {
	return b.South < b.North && b.South >= -tile.MaxLatitude && b.North <= tile.MaxLatitude &&
		b.West >= -180 && b.West <= 180 && b.East >= -180 && b.East <= 180
}

// lngSpan is the eastward extent in degrees, in [0, 360].
func (b Bounds) lngSpan() float64 {
	span := b.East - b.West
	if span < 0 {
		span += 360
	}
	return span
}

// contains reports whether lng lies within the box's longitude span.
func (b Bounds) contains(lng float64) bool {
	if b.West <= b.East {
		return lng >= b.West && lng <= b.East
	}
	return lng >= b.West || lng <= b.East
}

// Extend grows the box to include the point, widening the longitude span
// in whichever direction keeps it smaller.
func (b Bounds) Extend(lat, lng float64) Bounds {
	b.South = min(b.South, lat)
	b.North = max(b.North, lat)
	if b.contains(lng) {
		return b
	}
	east := Bounds{West: b.West, East: lng}.lngSpan()
	west := Bounds{West: lng, East: b.East}.lngSpan()
	if east <= west {
		b.East = lng
	} else {
		b.West = lng
	}
	return b
}

// fitBounds returns the center and the highest zoom, between minZoom and
// maxZoom, at which b fits inside a w×h pixel view at tileSize pixels
// per tile. Degenerate boxes fit at maxZoom.
func fitBounds(b Bounds, w, h, tileSize, maxZoom int) (lat, lng float64, zoom int) {
	south := max(-tile.MaxLatitude, min(tile.MaxLatitude, b.South))
	north := max(-tile.MaxLatitude, min(tile.MaxLatitude, b.North))
	if north < south {
		south, north = north, south
	}
	span := b.lngSpan()
	if span >= 360 {
		span = 360
	}
	// Fractions of the world at zoom 0.
	_, yNorth, world := tile.Project(north, 0, 0, tileSize)
	_, ySouth, _ := tile.Project(south, 0, 0, tileSize)
	fx := span / 360
	fy := (ySouth - yNorth) / world

	zoom = maxZoom
	for zoom > minZoom {
		size := world * math.Exp2(float64(zoom))
		if fx*size <= float64(w) && fy*size <= float64(h) {
			break
		}
		zoom--
	}
	lng = b.West + span/2
	if lng > 180 {
		lng -= 360
	}
	lat, _ = tile.Unproject(0, (yNorth+ySouth)/2, 0, tileSize)
	return lat, lng, zoom
}

// SetBounds centers the map on b and picks the highest zoom at which b
// fits inside the cell rectangle with a one-cell margin on each side.
// Like SetLatLng it changes the state only; call Init or another render
// trigger afterwards. An invalid box is ignored.
func (m *Model) SetBounds(b Bounds) {
	if !b.Valid() {
		return
	}
	w := max(1, (m.cols-2)*osmPxPerCellW)
	h := max(1, (m.picRows()-2)*osmPxPerCellH)
	m.lat, m.lng, m.zoom = fitBounds(b, w, h, m.info().Size(), m.info().Max())
}

// ContentBounds returns the box around every marker and path point, and
// false when there is no content.
func (m Model) ContentBounds() (Bounds, bool) {
	var b Bounds
	found := false
	add := func(lat, lng float64) {
		if !found {
			b, found = Bounds{South: lat, West: lng, North: lat, East: lng}, true
			return
		}
		b = b.Extend(lat, lng)
	}
	for _, mk := range m.markers {
		add(mk.Lat, mk.Lng)
	}
	for _, p := range m.paths {
		for _, pt := range p.Points {
			add(pt.Lat, pt.Lng)
		}
	}
	return b, found
}

// FitContent frames every marker and path, returning false when there is
// nothing to frame. A single point is shown at the source's highest zoom.
func (m *Model) FitContent() bool {
	b, ok := m.ContentBounds()
	if !ok {
		return false
	}
	if b.South == b.North {
		// Give a point or an east-west line some height to frame.
		b.South -= 1e-9
		b.North += 1e-9
	}
	m.SetBounds(b)
	return true
}
