package ntmaps

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"math"

	"nimbleterminal.dev/ntmaps/tile"
)

// Point is a geographic coordinate on a Path.
type Point struct{ Lat, Lng float64 }

// Path is a polyline to draw on the map: a track, a route, a boundary.
// Color and Width fall back to (blue, 3). Consecutive points take the
// short way around the antimeridian.
type Path struct {
	Points []Point
	Color  color.Color
	Width  float64
}

var defaultPathColor = color.RGBA{R: 0x1e, G: 0x66, B: 0xf5, A: 0xff}

// SetPaths replaces all currently-drawn paths. Call Init or any other
// render trigger afterwards, as with SetMarkers.
func (m *Model) SetPaths(paths []Path) {
	m.paths = paths
}

// ClearPaths removes all paths from the map.
func (m *Model) ClearPaths() {
	m.paths = nil
}

// pathsKey summarizes paths for the render cache key. Tracks can hold
// thousands of points, so the key carries a hash rather than the data.
func pathsKey(paths []Path) string {
	if len(paths) == 0 {
		return ""
	}
	h := fnv.New64a()
	var buf [8]byte
	put := func(f float64) {
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(f))
		h.Write(buf[:])
	}
	for _, p := range paths {
		put(p.Width)
		fmt.Fprintf(h, "%v|", p.Color)
		for _, pt := range p.Points {
			put(pt.Lat)
			put(pt.Lng)
		}
		h.Write([]byte{';'})
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// drawPaths draws each path as an anti-aliased stroke with round joins
// and caps. cx, cy are the world pixels of the view center at zoom and
// tileSize, matching the Cover call that produced img.
func drawPaths(img *image.RGBA, paths []Path, cx, cy float64, zoom, tileSize int) {
	if len(paths) == 0 {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	originX := math.Floor(cx) - float64(w/2)
	originY := math.Floor(cy) - float64(h/2)
	_, _, world := tile.Project(0, 0, zoom, tileSize)
	for _, path := range paths {
		if len(path.Points) < 2 {
			continue
		}
		col := path.Color
		if col == nil {
			col = defaultPathColor
		}
		width := path.Width
		if width <= 0 {
			width = 3
		}
		fill := color.RGBAModel.Convert(col).(color.RGBA)

		// Project every point, choosing the world copy nearest the
		// previous point so a track crossing the antimeridian stays
		// short; the first point sits nearest the view center.
		pts := make([]point, len(path.Points))
		prevX := cx
		for i, pt := range path.Points {
			mx, my, _ := tile.Project(pt.Lat, pt.Lng, zoom, tileSize)
			dx := mx - prevX
			dx -= math.Floor(dx/world+0.5) * world
			mx = prevX + dx
			prevX = mx
			pts[i] = point{mx - originX, my - originY}
		}
		for i := 1; i < len(pts); i++ {
			drawSegment(img, pts[i-1], pts[i], width/2, fill)
		}
	}
}

// drawSegment strokes the segment ab with the given half width, blending
// anti-aliased coverage from the distance to the segment.
func drawSegment(img *image.RGBA, a, b point, halfWidth float64, fill color.RGBA) {
	bounds := img.Bounds()
	pad := halfWidth + 1
	x0 := max(bounds.Min.X, int(math.Floor(min(a.x, b.x)-pad)))
	x1 := min(bounds.Max.X, int(math.Ceil(max(a.x, b.x)+pad)))
	y0 := max(bounds.Min.Y, int(math.Floor(min(a.y, b.y)-pad)))
	y1 := min(bounds.Max.Y, int(math.Ceil(max(a.y, b.y)+pad)))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	ab := b.sub(a)
	lenSq := ab.dot(ab)
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			p := point{float64(px) + 0.5, float64(py) + 0.5}
			t := 0.0
			if lenSq > 0 {
				t = clamp01(p.sub(a).dot(ab) / lenSq)
			}
			d := p.sub(a).sub(ab.scale(t)).length() - halfWidth
			if cov := clamp01(0.5 - d); cov > 0 {
				img.SetRGBA(px, py, blend(img.RGBAAt(px, py), fill, cov))
			}
		}
	}
}
