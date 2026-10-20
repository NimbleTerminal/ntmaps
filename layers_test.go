package ntmaps

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

var overlayRed = color.RGBA{R: 0xd0, G: 0x10, B: 0x10, A: 0xff}

// newOverlayTiles serves transparent tiles of the given size with a red
// square in the top-left corner, the shape of a seamark overlay.
func newOverlayTiles(t *testing.T, size int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size/8; y++ {
		for x := 0; x < size/8; x++ {
			img.SetRGBA(x, y, overlayRed)
		}
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, img)
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(encoded.Bytes())
	}))
	t.Cleanup(s.Close)
	return s, &requests
}

func overlayProvider(s *httptest.Server, size, maxZoom int) tile.Provider {
	return tile.Provider{Name: "marks", URL: s.URL + "/{z}/{x}/{y}.png", Attribution: "Marks (c) test", TileSize: size, MaxZoom: maxZoom}
}

// cornerCenter returns the coordinate that puts the top-left corner of
// tile (1205, 1540) at zoom 12, or its ancestor at a lower zoom, at image
// pixel (8, 8) of a w×h view, so the
// overlay's red square covers pixels 8..39 on both axes.
func cornerCenter(zoom, w, h int) (float64, float64) {
	tx, ty := 1205>>(12-zoom), 1540>>(12-zoom)
	x := float64(tx*256 + 8 + w/2)
	y := float64(ty*256 + 8 + h/2)
	return tile.Unproject(x, y, zoom, 256)
}

func TestOverlayDrawsOverTheBaseProgressively(t *testing.T) {
	base := newTestTiles(t)
	marks, markRequests := newOverlayTiles(t, 256)
	m := progressiveModel(t, base)
	lat, lng := cornerCenter(12, m.cols*osmPxPerCellW, m.picRows()*osmPxPerCellH)
	m.SetLatLng(lat, lng, 12)
	m.SetOverlays(overlayProvider(marks, 256, 19))
	msgs := runRender(t, m.renderMapCmd())
	m, _ = m.Update(msgs[len(msgs)-1])
	if got := color.RGBAModel.Convert(m.Image().At(10, 10)).(color.RGBA); got != overlayRed {
		t.Fatalf("overlay corner = %v, want red", got)
	}
	if got := centerPixel(m.Image()); got != tileGreen {
		t.Fatalf("center = %v, want the base to show through transparent overlay", got)
	}
	if markRequests.Load() == 0 {
		t.Fatal("overlay tiles were never fetched")
	}
	if v := m.View().Content; !strings.Contains(v, "Marks (c) test") {
		t.Fatalf("attribution row should include the overlay credit, got %q", v)
	}
	// A pan within the cached ring stays synchronous for both layers.
	before := markRequests.Load()
	m.panPixels(8, 0)
	m.renderMapCmd()
	if m.inFlight() {
		t.Fatal("pan over cached base and overlay tiles should be synchronous")
	}
	if markRequests.Load() != before {
		t.Fatal("synchronous pan fetched overlay tiles")
	}
}

func TestOverlaySkippedBeyondItsMaxZoomAndScaledWhenLarger(t *testing.T) {
	base := newTestTiles(t)
	marks, markRequests := newOverlayTiles(t, 512)
	m := progressiveModel(t, base)
	w, h := m.cols*osmPxPerCellW, m.picRows()*osmPxPerCellH
	lat, lng := cornerCenter(12, w, h)
	m.SetLatLng(lat, lng, 12)
	m.SetOverlays(overlayProvider(marks, 512, 10))
	msgs := runRender(t, m.renderMapCmd())
	m, _ = m.Update(msgs[len(msgs)-1])
	if markRequests.Load() != 0 {
		t.Fatal("overlay beyond its MaxZoom must not be fetched")
	}
	if v := m.View().Content; strings.Contains(v, "Marks (c) test") {
		t.Fatal("an overlay not in use should not be credited")
	}

	lat, lng = cornerCenter(9, w, h)
	m.SetLatLng(lat, lng, 9)
	msgs = runRender(t, m.renderMapCmd())
	m, _ = m.Update(msgs[len(msgs)-1])
	if markRequests.Load() == 0 {
		t.Fatal("overlay within its MaxZoom should be fetched")
	}
	if got := color.RGBAModel.Convert(m.Image().At(10, 10)).(color.RGBA); got.R < 150 || got.G > 100 {
		t.Fatalf("scaled overlay corner = %v, want red", got)
	}
}

func TestOverlayFailureKeepsTheBase(t *testing.T) {
	base := newTestTiles(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(down.Close)
	m := progressiveModel(t, base)
	m.SetOverlays(overlayProvider(down, 256, 19))
	msgs := runRender(t, m.renderMapCmd())
	last := msgs[len(msgs)-1]
	if last.err != nil {
		t.Fatalf("a failing overlay must not fail the render: %v", last.err)
	}
	if got := centerPixel(last.img); got != tileGreen {
		t.Fatalf("center = %v, want the base", got)
	}
}

func TestOverlaysChangeTheRenderKey(t *testing.T) {
	m := mouseTestModel()
	base := m.currentRenderKey()
	m.SetOverlays(tile.OpenSeaMap())
	if k := m.currentRenderKey(); k == base || k.overlays != "openseamap;" {
		t.Fatalf("overlay key = %q", k.overlays)
	}
	m.SetOverlays(tile.OpenSeaMap(), tile.OpenRailwayMap())
	if len(m.Overlays()) != 2 || m.currentRenderKey().overlays != "openseamap;openrailwaymap;" {
		t.Fatalf("overlays = %v", m.Overlays())
	}
	m.SetOverlays(nil, tile.OpenRailwayMap())
	if len(m.Overlays()) != 1 {
		t.Fatal("nil overlays should be dropped")
	}
	m.SetOverlays()
	if m.currentRenderKey() != base {
		t.Fatal("clearing overlays should restore the key")
	}
	for _, p := range []tile.Provider{tile.OpenSeaMap(), tile.OpenRailwayMap()} {
		if !p.Valid() || !strings.HasPrefix(p.TileURL(1, 1, 1), "https://") {
			t.Fatalf("%q: bad built-in overlay", p.Name)
		}
	}
}

func TestRenderWithOverlays(t *testing.T) {
	base := newTestTiles(t)
	marks, _ := newOverlayTiles(t, 256)
	lat, lng := cornerCenter(12, 300, 120)
	img, err := Render(context.Background(), RenderOptions{
		Source: base.provider(), Overlays: []tile.Source{overlayProvider(marks, 256, 19)},
		Loader: tile.NewLoader(tile.Options{}), Lat: lat, Lng: lng, Zoom: 12, Width: 300, Height: 120, NoAttribution: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := color.RGBAModel.Convert(img.At(10, 10)).(color.RGBA); got != overlayRed {
		t.Fatalf("overlay corner = %v, want red", got)
	}
	if got := color.RGBAModel.Convert(img.At(150, 60)).(color.RGBA); got != tileGreen {
		t.Fatalf("center = %v, want base", got)
	}
}
