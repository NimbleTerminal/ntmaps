package ntmaps

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

func solidTile(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestComposeTilesReportsMissingAndFillsPlaceholder(t *testing.T) {
	p := tile.Provider{Name: "t", URL: "x", Placeholder: color.RGBA{9, 9, 9, 255}}
	have := map[tile.ID]image.Image{{Z: 1, X: 0, Y: 0}: solidTile(color.RGBA{255, 0, 0, 255})}
	placements := []tile.Placement{
		{ID: tile.ID{Z: 1, X: 0, Y: 0}, Offset: image.Pt(0, 0), InWorld: true},
		{ID: tile.ID{Z: 1, X: 1, Y: 0}, Offset: image.Pt(256, 0), InWorld: true},
		{ID: tile.ID{Z: 1, X: 0, Y: -1}, Offset: image.Pt(0, -256), InWorld: false},
	}
	dst := image.NewRGBA(image.Rect(0, 0, 512, 256))
	missing := composeTiles(dst, placements, p.Info(), func(id tile.ID) (image.Image, bool) {
		img, ok := have[id]
		return img, ok
	})
	if len(missing) != 1 || missing[0] != (tile.ID{Z: 1, X: 1, Y: 0}) {
		t.Fatalf("missing = %v, want [{1 1 0}]", missing)
	}
	if got := dst.RGBAAt(10, 10); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("tile pixel = %v", got)
	}
	if got := dst.RGBAAt(300, 10); got != (color.RGBA{9, 9, 9, 255}) {
		t.Fatalf("missing tile pixel = %v, want placeholder", got)
	}
}

func TestComposeTilesClipsPartialTiles(t *testing.T) {
	p := tile.Provider{Name: "t", URL: "x"}
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	placements := tile.Cover(300.7, 300.7, 100, 100, 2, 256)
	missing := composeTiles(dst, placements, p.Info(), func(id tile.ID) (image.Image, bool) {
		if id == (tile.ID{Z: 2, X: 1, Y: 1}) {
			return solidTile(color.RGBA{0, 0, 255, 255}), true
		}
		return nil, false
	})
	if len(missing) != 3 {
		t.Fatalf("missing = %v, want three tiles", missing)
	}
	if got := dst.RGBAAt(99, 99); got != (color.RGBA{0, 0, 255, 255}) {
		t.Fatalf("bottom-right pixel = %v, want tile color", got)
	}
	if got := dst.RGBAAt(0, 0); got != color.RGBAModel.Convert(tile.DefaultPlaceholder).(color.RGBA) {
		t.Fatalf("top-left pixel = %v, want placeholder", got)
	}
}

func TestDrawMarkersPinAtCenter(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	cx, cy, _ := tile.Project(0, 0, 16, 256)
	drawMarkers(img, []Marker{{Lat: 0, Lng: 0}}, cx, cy, 16, 256)
	// The pin's head sits above its tip at the image center.
	if r, g, b, _ := img.At(64, 48).RGBA(); !(r > g && r > b) {
		t.Fatalf("expected red pin head at (64,48), got %v", img.At(64, 48))
	}
	if r, _, _, a := img.At(64, 70).RGBA(); a != 0 || r != 0 {
		t.Fatalf("expected nothing below the tip, got %v", img.At(64, 70))
	}
}

// TestDrawMarkersKeepsPinOverlappingViewport ports the go-staticmaps
// regression: a pin just outside the left edge with its right half still
// visible must be drawn, not wrapped to a distant world copy.
func TestDrawMarkersKeepsPinOverlappingViewport(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	cx, cy, _ := tile.Project(0, 0, 16, 256)
	lat, lng := tile.Unproject(cx-64-4, cy, 16, 256)
	drawMarkers(img, []Marker{{Lat: lat, Lng: lng}}, cx, cy, 16, 256)
	for y := 32; y < 64; y++ {
		for x := 0; x < 8; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > g && r > b {
				return
			}
		}
	}
	t.Fatal("partially visible pin was lost at the left edge")
}

func TestDrawMarkersSkipsPolarAndOffscreen(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	cx, cy, _ := tile.Project(0, 0, 10, 256)
	drawMarkers(img, []Marker{{Lat: 89, Lng: 0}, {Lat: 0, Lng: 90}}, cx, cy, 10, 256)
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			t.Fatal("expected no pixels for polar or offscreen markers")
		}
	}
}

func TestDrawPinOutlineAndFill(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	drawPin(img, 32, 48, 16, color.RGBA{R: 255, A: 255})
	// Head center is 16 above the tip; its edge sits 8 further out.
	if got := img.RGBAAt(32, 32); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("head center = %v, want fill", got)
	}
	edge := img.RGBAAt(32, 24)
	if edge.R > 90 || edge.G > 90 || edge.B > 90 {
		t.Fatalf("head edge = %v, want dark outline", edge)
	}
	if got := img.RGBAAt(32, 20); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("above the pin = %v, want untouched white", got)
	}
	tail := img.RGBAAt(32, 44)
	if tail.R < 200 || tail.G > 90 {
		t.Fatalf("tail interior = %v, want fill", tail)
	}
	if got := img.RGBAAt(20, 46); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("beside the tail = %v, want untouched white", got)
	}
}

func TestDrawPinClipsAtCorners(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	drawPin(img, 0, 0, 16, nil)
	drawPin(img, 16, 16, 16, color.RGBA{A: 255})
	drawPin(img, -40, -40, 16, color.RGBA{A: 255})
	var painted int
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			painted++
		}
	}
	if painted == 0 {
		t.Fatal("a pin overlapping the corner should paint something")
	}
}

func TestDrawMapAttributionStripAndClipping(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	drawMapAttribution(img, strings.Repeat("Maps and Data (c) openstreetmap.org ", 4))
	if got := img.RGBAAt(5, 5); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("top of image = %v, want untouched", got)
	}
	bottom := img.RGBAAt(39, 38)
	if bottom.R > 200 {
		t.Fatalf("bottom strip = %v, want darkened", bottom)
	}
	drawMapAttribution(img, "")
}
