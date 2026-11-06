package ntmaps

import (
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

// FuzzBoundsFit grows a box around a point and fits boxes to a view. A
// valid box extended by a point must still hold the point and its old
// extent, and fitting must land a zoom in range and a center inside the box,
// including across the antimeridian.
func FuzzBoundsFit(f *testing.F) {
	f.Add(40.0, -74.0, 41.0, -73.0, 40.5, 179.0, uint8(80), uint8(24))
	f.Add(-10.0, 170.0, 10.0, -170.0, 0.0, 180.0, uint8(1), uint8(1))
	f.Add(-85.0, -180.0, 85.0, 180.0, 0.0, 0.0, uint8(255), uint8(255))
	f.Fuzz(func(t *testing.T, south, west, north, east, lat, lng float64, w, h uint8) {
		b := Bounds{South: south, West: west, North: north, East: east}
		if !b.Valid() {
			return
		}
		if lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180 {
			e := b.Extend(lat, lng)
			if e.South > lat || e.North < lat || !e.contains(lng) {
				t.Fatalf("%+v extended by %g, %g is %+v, which misses the point", b, lat, lng, e)
			}
			if e.South > b.South || e.North < b.North || !e.contains(b.West) || !e.contains(b.East) {
				t.Fatalf("%+v extended by %g, %g is %+v, which lost the box", b, lat, lng, e)
			}
		}
		width, height := (int(w)+1)*osmPxPerCellW, (int(h)+1)*osmPxPerCellH
		clat, clng, zoom := fitBounds(b, width, height, 256, 19)
		if zoom < minZoom || zoom > 19 {
			t.Fatalf("fit %+v to %d×%d at zoom %d", b, width, height, zoom)
		}
		if math.IsNaN(clat) || math.Abs(clat) > tile.MaxLatitude || math.IsNaN(clng) || math.Abs(clng) > 180 {
			t.Fatalf("fit %+v centers on %g, %g", b, clat, clng)
		}
		const eps = 1e-9
		if clat < min(b.South, tile.MaxLatitude)-eps || clat > b.North+eps {
			t.Fatalf("fit %+v centers at latitude %g outside the box", b, clat)
		}
		if !b.contains(clng) && !b.contains(clng-eps) && !b.contains(clng+eps) {
			t.Fatalf("fit %+v centers at longitude %g outside the box", b, clng)
		}
	})
}

// FuzzDrawPaths strokes paths through arbitrary coordinates and widths
// onto a small canvas. The rasterizer must finish and stay within the
// image whatever it is handed, including NaN, infinities, and a path that
// crosses the antimeridian or a pole.
func FuzzDrawPaths(f *testing.F) {
	f.Add(40.7415, -73.989, 40.75, -73.98, 40.76, -74.0, 3.0, uint8(12))
	f.Add(0.0, 179.9, 0.0, -179.9, 1.0, 180.0, 1.5, uint8(2))
	f.Add(math.NaN(), math.Inf(1), 90.0, -180.0, -90.0, 180.0, math.Inf(1), uint8(0))
	f.Fuzz(func(t *testing.T, lat1, lng1, lat2, lng2, lat3, lng3, width float64, zoom uint8) {
		z := int(zoom % 23)
		img := image.NewRGBA(image.Rect(0, 0, 48, 32))
		cx, cy, _ := tile.Project(lat1, lng1, z, 256)
		paths := []Path{
			{Points: []Point{{lat1, lng1}, {lat2, lng2}, {lat3, lng3}}, Width: width},
			{Points: []Point{{lat2, lng2}, {lat3, lng3}}, Color: color.NRGBA{R: 255, A: 128}, Width: -width},
			{Points: []Point{{lat3, lng3}}},
		}
		drawPaths(img, paths, cx, cy, z, 256)
		if pathsKey(paths) != pathsKey(paths) {
			t.Fatal("pathsKey is not deterministic")
		}
	})
}
