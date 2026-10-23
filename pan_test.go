package ntmaps

import (
	"image"
	"image/color"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"nimbleterminal.dev/ntmaps/tile"
	picture "nimbleterminal.dev/ntpicture"
)

// animatedTestModel pans with the spring over a provider that has no
// tiles, so every frame composes synchronously and nothing is fetched.
func animatedTestModel() Model {
	m := NewWithConfig(Config{Cols: 20, Rows: 11, SmoothPan: true, DisableDiskCache: true, Prefetch: -1})
	m.SetLatLng(0, 0, 15)
	m.source = tile.Provider{}
	return m
}

func TestPrefetchRingMakesNeighborPanSynchronous(t *testing.T) {
	s := newTestTiles(t)
	m := NewWithConfig(Config{Cols: 20, Rows: 5, RenderDebounce: -1, DisableDiskCache: true})
	m.SetLatLng(40.7415, -73.989, 12)
	m.SetTileSource(s.provider())
	msgs := runRender(t, m.renderMapCmd())
	m, cmd := m.Update(msgs[len(msgs)-1])
	if cmd == nil {
		t.Fatal("completing a render should schedule the prefetch")
	}
	before := s.requests.Load()

	// Run the prefetch directly (Update wrapped it in a Batch).
	geom := viewGeometry{source: m.source, zoom: m.zoom, w: m.cols * osmPxPerCellW, h: m.picRows() * osmPxPerCellH}
	geom.cx, geom.cy, _ = tile.Project(m.lat, m.lng, m.zoom, 256)
	prefetch := m.prefetchCmd(geom)
	if prefetch == nil {
		t.Fatal("expected ring tiles to fetch")
	}
	_ = prefetch()
	after := s.requests.Load()
	if after <= before {
		t.Fatal("prefetch fetched nothing")
	}

	// A full tile width in any direction now composes from memory.
	for _, d := range [][2]float64{{256, 0}, {-512, 0}, {256, 256}, {0, -512}} {
		m.panPixels(d[0], d[1])
		m.renderMapCmd()
		if m.inFlight() || m.job.current != nil {
			t.Fatalf("pan by %v was not synchronous", d)
		}
		if got := centerPixel(m.Image()); got != tileGreen {
			t.Fatalf("pan by %v: center = %v, want tile color", d, got)
		}
	}
	if n := s.requests.Load(); n != after {
		t.Fatalf("pans within the ring fetched %d tiles", n-after)
	}
	if m.prefetchCmd(geom) != nil {
		t.Fatal("a fully cached ring should need no prefetch")
	}
}

func TestPrefetchIsCanceledByNewRender(t *testing.T) {
	s := newTestTiles(t)
	s.hold = make(chan struct{})
	defer close(s.hold)
	m := NewWithConfig(Config{Cols: 20, Rows: 5, RenderDebounce: -1, DisableDiskCache: true})
	m.SetLatLng(40.7415, -73.989, 12)
	m.SetTileSource(s.provider())
	geom := viewGeometry{source: m.source, zoom: m.zoom, w: 160, h: 64}
	geom.cx, geom.cy, _ = tile.Project(m.lat, m.lng, m.zoom, 256)
	prefetch := m.prefetchCmd(geom)
	done := make(chan struct{})
	go func() { defer close(done); _ = prefetch() }()
	time.Sleep(20 * time.Millisecond)
	m.SetLatLng(51.5, -0.12, 12)
	m.renderMapCmd()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("prefetch kept running after a new render")
	}
	if m.job.prefetch != nil && m.job.current == nil {
		t.Fatal("job slot should not keep a canceled prefetch without a render")
	}
}

func TestNoPrefetchWhenDisabledOrEmptyProvider(t *testing.T) {
	m := NewWithConfig(Config{Cols: 20, Rows: 5, Prefetch: -1, DisableDiskCache: true})
	if m.prefetch != 0 {
		t.Fatalf("prefetch rings = %d, want 0", m.prefetch)
	}
	if NewWithConfig(Config{Prefetch: 9}).prefetch != 4 || New(1, 1).prefetch != 1 {
		t.Fatal("prefetch should clamp to 1..4")
	}
	m.SetTileSource(tile.Provider{})
	geom := viewGeometry{source: m.source, w: 160, h: 64}
	if m.prefetchCmd(geom) != nil {
		t.Fatal("empty provider should not prefetch")
	}
}

func TestPanUsesScreenPixelsAtDifferentLatitudes(t *testing.T) {
	for _, lat := range []float64{0, 60, 80} {
		m := animatedTestModel()
		m.SetLatLng(lat, 10, 15)
		x, y, _ := worldPixels(m.lat, m.lng, m.zoom, 256)
		m.panPixels(16, -16)
		nx, ny, _ := worldPixels(m.lat, m.lng, m.zoom, 256)
		if math.Abs(nx-x-16) > 1e-6 || math.Abs(ny-y+16) > 1e-6 {
			t.Fatalf("latitude %g: pan moved (%g,%g) pixels", lat, nx-x, ny-y)
		}
	}
	m := animatedTestModel()
	m.SetLatLng(tile.MaxLatitude, 0, 15)
	m.panPixels(0, -16)
	if math.Abs(m.lat-tile.MaxLatitude) > 1e-9 {
		t.Fatal("northward pan should clamp at the Mercator boundary")
	}
}

func TestKittyPresentsFramesDuringRapidInput(t *testing.T) {
	m := animatedTestModel()
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m.pic.SetMode(RenderKitty)
	// A marker moves across the placeholder, so each pan is a new image.
	m.SetMarkers([]Marker{{Lat: 0, Lng: 0}})
	first := m.renderMapCmd()
	if first == nil {
		t.Fatal("expected a presentation Cmd in Kitty mode")
	}
	for i := 0; i < 5; i++ {
		m.panPixels(16, 0)
		if cmd := m.renderMapCmd(); cmd != nil {
			t.Fatal("pan invalidated the encoding in flight")
		}
	}
	if m.panState.pending == nil {
		t.Fatal("latest image should be queued behind the frame in flight")
	}
	firstGeneration := m.pic.Status().Generation
	latestImage := m.panState.pending
	drainMap(t, &m, first)
	status := m.pic.Status()
	if status.Busy || status.Presented != firstGeneration+1 || status.Presented != status.Generation {
		t.Fatalf("expected exactly two completed presentations: %+v", status)
	}
	if m.panState.pending != nil || latestImage != m.sourceImage {
		t.Fatal("queue did not settle on newest source")
	}

}

func TestAnimationFramesSkipDebounceAndRenderCache(t *testing.T) {
	s := newTestTiles(t)
	s.hold = make(chan struct{})
	defer close(s.hold)
	m := NewWithConfig(Config{Cols: 20, Rows: 5, SmoothPan: true, DisableDiskCache: true, Prefetch: -1})
	m.SetLatLng(40.7415, -73.989, 12)
	m.SetTileSource(s.provider())
	m, _ = m.arrowPan(16, 0)
	if !m.animating() {
		t.Fatal("arrow should start the spring")
	}
	cmd := m.renderMapCmd()
	if cmd == nil {
		t.Fatal("expected a fetch Cmd")
	}
	if _, ok := cmd().(mapImageMsg); !ok {
		t.Fatal("an animating render should dispatch its job without a debounce tick")
	}
	if len(m.cache.entries) != 0 {
		t.Fatal("frames during the animation must not fill the render cache")
	}
}

type countingImage struct {
	image.Image
	reads atomic.Int64
}

func (i *countingImage) At(x, y int) color.Color {
	i.reads.Add(1)
	return i.Image.At(x, y)
}

func TestGlyphCacheSurvivesValueReceiverView(t *testing.T) {
	m := New(20, 11)
	src := &countingImage{Image: newSolidImage(color.RGBA{90, 40, 30, 255})}
	drainMap(t, &m, m.pic.SetImageImmutable(src))
	first := m.View().Content
	reads := src.reads.Load()
	if reads == 0 {
		t.Fatal("first view did not render the source")
	}
	if m.View().Content != first || src.reads.Load() != reads {
		t.Fatal("unchanged View re-rendered the source instead of using the glyph cache")
	}
}

func BenchmarkCachedPan(b *testing.B) {
	m := animatedTestModel()
	m.renderMapCmd()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		dx := 16.0
		if i%2 != 0 {
			dx = -16
		}
		m.panPixels(dx, 0)
		m.renderMapCmd()
	}
}
