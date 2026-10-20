package ntmaps

import (
	"context"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

// fits reports whether every corner of b lands inside a w×h view centered
// on (lat, lng) at zoom.
func fits(b Bounds, lat, lng float64, zoom, w, h int) bool {
	cx, cy, world := tile.Project(lat, lng, zoom, 256)
	for _, c := range [][2]float64{{b.South, b.West}, {b.North, b.East}, {b.South, b.East}, {b.North, b.West}} {
		x, y, _ := tile.Project(c[0], c[1], zoom, 256)
		dx := x - cx
		dx -= math.Floor(dx/world+0.5) * world
		if math.Abs(dx) > float64(w)/2 || math.Abs(y-cy) > float64(h)/2 {
			return false
		}
	}
	return true
}

func TestSetBoundsPicksTheLargestZoomThatFits(t *testing.T) {
	m := mouseTestModel() // 20×11 cells → 18×8 cells inside the margin
	b := Bounds{South: 40.70, West: -74.05, North: 40.80, East: -73.95}
	m.SetBounds(b)
	w, h := 18*osmPxPerCellW, 8*osmPxPerCellH
	lat, lng := m.Center()
	if !fits(b, lat, lng, m.Zoom(), w, h) {
		t.Fatalf("box does not fit at zoom %d centered on %g,%g", m.Zoom(), lat, lng)
	}
	if fits(b, lat, lng, m.Zoom()+1, w, h) {
		t.Fatalf("zoom %d is not the largest that fits", m.Zoom())
	}
	if math.Abs(lng+74) > 1e-9 || lat <= 40.70 || lat >= 40.80 {
		t.Fatalf("center = %g,%g", lat, lng)
	}
}

func TestSetBoundsAcrossTheAntimeridian(t *testing.T) {
	m := mouseTestModel()
	b := Bounds{South: -20, West: 170, North: -10, East: -170}
	m.SetBounds(b)
	lat, lng := m.Center()
	if math.Abs(math.Abs(lng)-180) > 1e-9 {
		t.Fatalf("center lng = %g, want ±180", lng)
	}
	if !fits(b, lat, lng, m.Zoom(), 18*osmPxPerCellW, 8*osmPxPerCellH) {
		t.Fatalf("box does not fit at zoom %d", m.Zoom())
	}
	if m.Zoom() < 3 {
		t.Fatalf("zoom %d is too far out for a 20° box", m.Zoom())
	}
}

func TestSetBoundsClampsAndIgnoresInvalid(t *testing.T) {
	m := mouseTestModel()
	m.SetTileSource(tile.Provider{Name: "shallow", URL: "x/{z}/{x}/{y}", MaxZoom: 6})
	m.SetBounds(Bounds{South: 40.7, West: -74, North: 40.7001, East: -73.9999})
	if m.Zoom() != 6 {
		t.Fatalf("tiny box should sit at the source max 6, got %d", m.Zoom())
	}
	m.SetBounds(Bounds{South: -85, West: -180, North: 85, East: 180})
	if m.Zoom() != minZoom {
		t.Fatalf("the whole world should sit at the floor %d, got %d", minZoom, m.Zoom())
	}
	before := m.Zoom()
	m.SetBounds(Bounds{South: 10, North: 5})
	if m.Zoom() != before {
		t.Fatal("an invalid box should be ignored")
	}
}

func TestFitContentFramesMarkersAndPaths(t *testing.T) {
	m := mouseTestModel()
	if m.FitContent() {
		t.Fatal("nothing to frame")
	}
	m.SetMarkers([]Marker{{Lat: 48.85, Lng: 2.35}})
	m.SetPaths([]Path{{Points: []Point{{Lat: 51.5, Lng: -0.12}, {Lat: 52.37, Lng: 4.9}}}})
	b, ok := m.ContentBounds()
	if !ok || b != (Bounds{South: 48.85, West: -0.12, North: 52.37, East: 4.9}) {
		t.Fatalf("content bounds = %+v", b)
	}
	if !m.FitContent() {
		t.Fatal("expected content to frame")
	}
	lat, lng := m.Center()
	if !fits(b, lat, lng, m.Zoom(), 18*osmPxPerCellW, 8*osmPxPerCellH) {
		t.Fatalf("content does not fit at zoom %d", m.Zoom())
	}
	m.ClearPaths()
	m.FitContent()
	if m.Zoom() != m.info().Max() {
		t.Fatalf("a lone marker should frame at max zoom, got %d", m.Zoom())
	}
}

func TestBoundsExtendTakesTheShortWay(t *testing.T) {
	b := Bounds{South: 0, West: 170, North: 0, East: 170}
	b = b.Extend(1, -175)
	if b.West != 170 || b.East != -175 || b.lngSpan() != 15 {
		t.Fatalf("extend across the dateline = %+v span %g", b, b.lngSpan())
	}
	b = b.Extend(-1, 175)
	if b.West != 170 || b.East != -175 || b.South != -1 || b.North != 1 {
		t.Fatalf("point inside the span changed longitude: %+v", b)
	}
}

func TestRenderWithBounds(t *testing.T) {
	s := newTestTiles(t)
	b := Bounds{South: 40.70, West: -74.05, North: 40.80, East: -73.95}
	img, err := Render(context.Background(), RenderOptions{Source: s.provider(), Loader: tile.NewLoader(tile.Options{}), Bounds: &b, Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got := color.RGBAModel.Convert(img.At(100, 50)).(color.RGBA); got != tileGreen {
		t.Fatalf("center = %v", got)
	}
	if _, err := Render(context.Background(), RenderOptions{Source: s.provider(), Bounds: &Bounds{South: 1, North: 0}, Width: 10, Height: 10}); err == nil {
		t.Fatal("expected an invalid-bounds error")
	}
}
