package ntmaps

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
	picture "nimbleterminal.dev/ntpicture"
)

func kittyPanModel(t *testing.T) Model {
	t.Helper()
	session := picture.NewTerminalSession(picture.TerminalOptions{
		Kitty:            picture.KittyCapabilitySupported,
		VirtualPlacement: picture.KittyCapabilitySupported,
		SharedMemory:     picture.KittyCapabilityUnsupported,
	})
	t.Cleanup(session.Close)
	m := NewWithConfig(Config{Cols: 20, Rows: 11, SmoothPan: true, DisableDiskCache: true, Prefetch: -1, Session: session})
	m.SetLatLng(0, 0, 15)
	m.source = tile.Provider{}
	t.Cleanup(m.Close)
	m.pic.SetMode(RenderKitty)
	return m
}

func assertPlacementOnly(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "a=p") || strings.Contains(out, "a=T") || strings.Contains(out, "a=t") || len(out) > 256 {
		t.Fatalf("expected a small placement update without image data: %d bytes, %.256q", len(out), out)
	}
}

func TestCachedPanReusesUploadAndPlaceholderGrid(t *testing.T) {
	m := kittyPanModel(t)
	m.SetMarkers([]Marker{{Lat: 0, Lng: 0}})
	first := drainMap(t, &m, m.renderMapCmd())
	if !strings.Contains(first, "a=T") || m.panState.canvas == nil {
		t.Fatal("initial canvas was not uploaded")
	}
	canvas, grid := m.panState.canvas, m.pic.View()
	m.panPixels(16, -16)
	assertPlacementOnly(t, drainMap(t, &m, m.renderMapCmd()))
	if m.panState.canvas != canvas || m.pic.View() != grid || m.inFlight() {
		t.Fatal("pan replaced canvas/grid or left a render pending")
	}
	assertPlacementOnly(t, drainMap(t, &m, m.SetOpticalZoom(1)))
	assertPlacementOnly(t, drainMap(t, &m, m.SetOpticalZoom(0)))
	// Exported pixels retain the ordinary viewport dimensions and marker position.
	g := canvas.geom
	g.w, g.h = m.cols*osmPxPerCellW, m.picRows()*osmPxPerCellH
	g.cellRectW, g.cellRectH = g.w, g.h
	g.cx, g.cy, _ = tile.Project(m.lat, m.lng, g.zoom, m.info().Size())
	want, _ := g.compose(g.placements(), func(tile.Source, tile.ID) (image.Image, bool) { return nil, false })
	if got := m.Image().(*image.RGBA); got.Bounds() != want.Bounds() || !bytes.Equal(got.Pix, want.(*image.RGBA).Pix) {
		t.Fatal("canvas crop differs from a fresh viewport render")
	}
}

func TestCachedPanRefreshesChangedContentAndCanvasEdges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Model)
	}{
		{"edge", func(m *Model) { m.panPixels(1000, 0) }},
		{"marker", func(m *Model) { m.SetMarkers([]Marker{{Lat: 0, Lng: 0}}) }},
		{"zoom", func(m *Model) { m.zoom++ }},
		{"source", func(m *Model) { m.setSource(tile.Provider{Name: "other"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := kittyPanModel(t)
			drainMap(t, &m, m.renderMapCmd())
			before := m.panState.canvas
			tc.change(&m)
			out := drainMap(t, &m, m.renderMapCmd())
			if before == m.panState.canvas || !strings.Contains(out, "a=T") {
				t.Fatal("changed render input reused stale canvas pixels")
			}
		})
	}
}

func TestCachedPanWrapsAntimeridian(t *testing.T) {
	m := kittyPanModel(t)
	m.SetLatLng(0, 179.9999, 15)
	drainMap(t, &m, m.renderMapCmd())
	before := m.panState.canvas
	m.panPixels(16, 0)
	assertPlacementOnly(t, drainMap(t, &m, m.renderMapCmd()))
	if m.lng >= 0 || m.panState.canvas != before {
		t.Fatal("antimeridian crossing did not reuse the neighboring world pixels")
	}
}

func TestCachedPanMissingTilesDoesNotFetchOverscan(t *testing.T) {
	s := newTestTiles(t)
	m := kittyPanModel(t)
	m.setSource(s.provider())
	m.renderDebounce = -1
	cmd := m.renderMapCmd()
	if m.panState.canvas != nil || m.job.current == nil {
		t.Fatal("missing tiles should use the normal progressive render")
	}
	if g := m.job.current.geom; g.w != m.cols*osmPxPerCellW || g.h != m.picRows()*osmPxPerCellH {
		t.Fatal("render expanded the network fetch area")
	}
	if cmd == nil || s.requests.Load() != 0 {
		t.Fatal("scheduled render fetched tiles before its command ran")
	}

}

func TestCachedPanLetterboxAndGlyphFallback(t *testing.T) {
	m := kittyPanModel(t)
	m.maxAspectRatio = 1
	m.cols = 80
	m.renderMapCmd()
	if m.panState.canvas != nil {
		t.Fatal("letterboxed view used a pan canvas")
	}
	m.maxAspectRatio = 0
	m.pic.SetMode(RenderGlyph)
	m.renderMapCmd()
	if m.panState.canvas != nil {
		t.Fatal("glyph view used a pan canvas")
	}
}
