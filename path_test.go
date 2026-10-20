package ntmaps

import (
	"image"
	"image/color"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

var pathRed = color.RGBA{R: 255, A: 255}

func pointAt(cx, cy, dx, dy float64, zoom int) Point {
	lat, lng := tile.Unproject(cx+dx, cy+dy, zoom, 256)
	return Point{Lat: lat, Lng: lng}
}

func TestDrawPathsStrokesAcrossTheView(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 64))
	cx, cy, _ := tile.Project(0, 0, 4, 256)
	// A horizontal line through the center, 60 px wide at zoom 4.
	path := Path{Points: []Point{pointAt(cx, cy, -30, 0, 4), pointAt(cx, cy, 30, 0, 4)}, Color: pathRed, Width: 4}
	drawPaths(img, []Path{path}, cx, cy, 4, 256)
	for x := 40; x < 90; x++ {
		if got := img.RGBAAt(x, 32); got.R < 200 || got.G > 60 {
			t.Fatalf("pixel (%d,32) = %v, want red stroke", x, got)
		}
	}
	if got := img.RGBAAt(64, 20); got.A != 0 {
		t.Fatalf("pixel above the stroke = %v, want untouched", got)
	}
	if got := img.RGBAAt(10, 32); got.A != 0 {
		t.Fatalf("pixel beyond the end cap = %v, want untouched", got)
	}
}

func TestDrawPathsTakesTheShortWayAcrossTheAntimeridian(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 64))
	cx, cy, _ := tile.Project(0, 180, 3, 256)
	drawPaths(img, []Path{{Points: []Point{{Lat: 0, Lng: 179.5}, {Lat: 0, Lng: -179.5}}, Color: pathRed, Width: 2}}, cx, cy, 3, 256)
	if got := img.RGBAAt(128, 32); got.R < 200 {
		t.Fatalf("center pixel = %v, want the short segment across the dateline", got)
	}
	if got := img.RGBAAt(20, 32); got.A != 0 {
		t.Fatalf("pixel far from the dateline = %v, want untouched (segment went the long way)", got)
	}
}

func TestDrawPathsSkipsDegenerateAndDefaults(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	cx, cy, _ := tile.Project(0, 0, 2, 256)
	drawPaths(img, []Path{{Points: []Point{{Lat: 0, Lng: 0}}}, {}}, cx, cy, 2, 256)
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			t.Fatal("a single point or empty path should draw nothing")
		}
	}
	drawPaths(img, []Path{{Points: []Point{pointAt(cx, cy, -8, 0, 2), pointAt(cx, cy, 8, 0, 2)}}}, cx, cy, 2, 256)
	if got := img.RGBAAt(16, 16); got != defaultPathColor {
		t.Fatalf("default stroke = %v, want %v", got, defaultPathColor)
	}
}

func TestPathsChangeTheRenderKey(t *testing.T) {
	m := mouseTestModel()
	base := m.currentRenderKey()
	m.SetPaths([]Path{{Points: []Point{{Lat: 0, Lng: 0}, {Lat: 1, Lng: 1}}}})
	withPath := m.currentRenderKey()
	if withPath == base || withPath.paths == "" {
		t.Fatal("paths should participate in the render key")
	}
	m.SetPaths([]Path{{Points: []Point{{Lat: 0, Lng: 0}, {Lat: 1, Lng: 1.0001}}}})
	if m.currentRenderKey() == withPath {
		t.Fatal("a different path should produce a different key")
	}
	m.ClearPaths()
	if m.currentRenderKey() != base {
		t.Fatal("clearing paths should restore the key")
	}
	m.SetPaths([]Path{{Points: []Point{{Lat: 10, Lng: 20}, {Lat: 10.01, Lng: 20.01}}, Color: pathRed, Width: 2}})
	m.renderMapCmd()
	if m.Image() == nil {
		t.Fatal("expected a composed image")
	}
}
