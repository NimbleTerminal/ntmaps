package ntmaps

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/geocode"
	"nimbleterminal.dev/ntmaps/tile"
)

var tileGreen = color.RGBA{R: 0x10, G: 0xc0, B: 0x20, A: 0xff}

// testTiles serves a solid green 256×256 PNG for every tile. When fail is
// set every request returns 500; when hold is non-nil requests block
// until it is closed.
type testTiles struct {
	*httptest.Server
	requests atomic.Int32
	fail     atomic.Bool
	hold     chan struct{}
}

func newTestTiles(t *testing.T) *testTiles {
	t.Helper()
	s := &testTiles{}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, solidTile(tileGreen))
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		if s.hold != nil {
			select {
			case <-s.hold:
			case <-r.Context().Done():
				return
			}
		}
		if s.fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(encoded.Bytes())
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *testTiles) provider() tile.Provider {
	return tile.Provider{Name: "test", URL: s.URL + "/{z}/{x}/{y}.png", Attribution: "", Placeholder: color.RGBA{9, 9, 9, 255}}
}

func progressiveModel(t *testing.T, s *testTiles) Model {
	t.Helper()
	m := NewWithConfig(Config{Cols: 20, Rows: 5, RenderDebounce: -1, DisableDiskCache: true})
	m.SetLatLng(40.7415, -73.989, 12)
	m.SetTileSource(s.provider())
	return m
}

// runRender executes the fetch Cmd and every next Cmd it chains,
// returning each message in order.
func runRender(t *testing.T, cmd tea.Cmd) []mapImageMsg {
	t.Helper()
	var msgs []mapImageMsg
	for cmd != nil {
		raw := cmd()
		msg, ok := raw.(mapImageMsg)
		if !ok {
			t.Fatalf("render produced %T, want mapImageMsg", raw)
		}
		msgs = append(msgs, msg)
		if msg.partial {
			cmd = msg.next
			continue
		}
		cmd = nil
		if len(msgs) > 100 {
			t.Fatal("render never completed")
		}
	}
	return msgs
}

func centerPixel(img image.Image) color.RGBA {
	b := img.Bounds()
	return color.RGBAModel.Convert(img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2)).(color.RGBA)
}

func TestProgressiveRenderPaintsTilesAsTheyArrive(t *testing.T) {
	s := newTestTiles(t)
	m := progressiveModel(t, s)
	cmd := m.renderMapCmd()
	if cmd == nil {
		t.Fatal("expected a fetch Cmd for uncached tiles")
	}
	if !m.inFlight() {
		t.Fatal("render should be in flight before any tile lands")
	}
	msgs := runRender(t, cmd)
	first, last := msgs[0], msgs[len(msgs)-1]
	if !first.partial || first.next == nil {
		t.Fatalf("first message should be partial with a continuation, got %+v", first)
	}
	if got := centerPixel(first.img); got != (color.RGBA{9, 9, 9, 255}) {
		t.Fatalf("first image center = %v, want placeholder", got)
	}
	if last.partial || last.err != nil {
		t.Fatalf("last message should be complete, got %+v", last)
	}
	if got := centerPixel(last.img); got != tileGreen {
		t.Fatalf("final image center = %v, want tile color", got)
	}

	m, _ = m.Update(first)
	if !m.inFlight() || m.Image() == nil {
		t.Fatal("partial image should display while the render stays in flight")
	}
	m, _ = m.Update(last)
	if m.inFlight() {
		t.Fatal("complete image should end the in-flight state")
	}
	if got := centerPixel(m.Image()); got != tileGreen {
		t.Fatalf("displayed image center = %v, want tile color", got)
	}
	if m.job.current != nil {
		t.Fatal("finished job should be released")
	}
}

func TestCachedTilesRenderSynchronously(t *testing.T) {
	s := newTestTiles(t)
	m := progressiveModel(t, s)
	msgs := runRender(t, m.renderMapCmd())
	m, _ = m.Update(msgs[len(msgs)-1])
	requests := s.requests.Load()

	// A small pan stays within the fetched tiles: no job, no badge, new image.
	m.panPixels(8, 0)
	m.renderMapCmd()
	if m.inFlight() || m.job.current != nil {
		t.Fatal("pan across cached tiles should complete synchronously")
	}
	if got := centerPixel(m.Image()); got != tileGreen {
		t.Fatalf("panned image center = %v, want tile color", got)
	}
	if n := s.requests.Load(); n != requests {
		t.Fatalf("synchronous render fetched %d more tiles", n-requests)
	}
}

func TestEmptyProviderRendersPlaceholderWithoutFetching(t *testing.T) {
	m := NewWithConfig(Config{Cols: 20, Rows: 5, RenderDebounce: -1, DisableDiskCache: true})
	m.SetTileSource(tile.Provider{Placeholder: color.RGBA{1, 2, 3, 255}})
	m.renderMapCmd()
	if m.inFlight() {
		t.Fatal("nothing to fetch should not be in flight")
	}
	if got := centerPixel(m.Image()); got != (color.RGBA{1, 2, 3, 255}) {
		t.Fatalf("image center = %v, want placeholder", got)
	}
}

func TestSupersededRenderCancelsItsJob(t *testing.T) {
	s := newTestTiles(t)
	s.hold = make(chan struct{})
	defer close(s.hold)
	m := progressiveModel(t, s)
	cmd := m.renderMapCmd()
	first := cmd().(mapImageMsg)
	old := m.job.current
	if old == nil || old.ctx.Err() != nil {
		t.Fatal("expected a live job for the first render")
	}
	m, _ = m.Update(first)

	m.SetLatLng(51.5, -0.12, 12)
	if cmd := m.renderMapCmd(); cmd == nil {
		t.Fatal("expected a fetch Cmd for the second render")
	}
	if old.ctx.Err() == nil {
		t.Fatal("superseded job should be canceled")
	}
	if m.job.current == old {
		t.Fatal("job slot should hold the new render")
	}
	// The old job's continuation returns nil and its stale image is dropped.
	done := make(chan struct{})
	go func() { defer close(done); _ = first.next() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled job's next did not return")
	}
	before := m.Image()
	m, _ = m.Update(mapImageMsg{gen: first.gen, img: solidTile(tileGreen)})
	if m.Image() != before {
		t.Fatal("stale render replaced the image")
	}
}

func TestAllTileFetchesFailingReportsError(t *testing.T) {
	s := newTestTiles(t)
	s.fail.Store(true)
	m := progressiveModel(t, s)
	msgs := runRender(t, m.renderMapCmd())
	last := msgs[len(msgs)-1]
	if last.err == nil {
		t.Fatalf("expected an error when no tile loads, got %+v", last)
	}
	m, _ = m.Update(last)
	if m.inFlight() || m.View().Content == "" || m.Image() != nil {
		t.Fatal("error should end the render and clear the image")
	}
}

func TestSetTileSourceSwitchesAndAttributes(t *testing.T) {
	m := New(40, 3)
	if m.TileSource().Info().Name != tile.OpenStreetMap().Name {
		t.Fatalf("default source = %q", m.TileSource().Info().Name)
	}
	m.SetTileSource(tile.ArcGISWorldImagery())
	if m.TileSource().Info().Name != tile.ArcGISWorldImagery().Name {
		t.Fatalf("after SetTileSource: source = %q", m.TileSource().Info().Name)
	}
	m, _ = m.Update(mapImageMsg{gen: *m.renderGen, img: solidTile(tileGreen)})
	if v := m.View().Content; !strings.Contains(v, "Esri") || strings.Contains(v, "openstreetmap") {
		t.Fatalf("attribution row should follow the source, got %q", v)
	}
	m.SetTileSource(nil)
	m, _ = m.Update(mapImageMsg{gen: *m.renderGen, img: solidTile(tileGreen)})
	if rows := strings.Count(m.View().Content, "\n") + 1; rows != 3 {
		t.Fatalf("a source without attribution should still fill %d rows, got %d", 3, rows)
	}
}

func TestOversampleCapsAtProviderMaxZoom(t *testing.T) {
	m := NewWithConfig(Config{Cols: 10, Rows: 3, Oversample: 4, DisableDiskCache: true})
	m.SetTileSource(tile.Provider{Name: "lowzoom", URL: "x/{z}/{x}/{y}", MaxZoom: 10})
	m.SetLatLng(0, 0, 10)
	if factor, boost := m.effectiveOversample(); factor != 1 || boost != 0 {
		t.Fatalf("effectiveOversample at the provider cap = %d/%d, want 1/0", factor, boost)
	}
	m.SetLatLng(0, 0, 8)
	if factor, boost := m.effectiveOversample(); factor != 4 || boost != 2 {
		t.Fatalf("effectiveOversample below the cap = %d/%d, want 4/2", factor, boost)
	}
}

func TestSetLocationUsesConfiguredGeocoder(t *testing.T) {
	var asked string
	g := geocode.Func(func(ctx context.Context, q string) (geocode.Result, error) {
		asked = q
		if q == "nowhere" {
			return geocode.Result{}, geocode.ErrNotFound
		}
		return geocode.Result{Lat: 51.5, Lng: -0.12}, nil
	})
	m := NewWithConfig(Config{Cols: 20, Rows: 5, Geocoder: g, DisableDiskCache: true})
	m.SetTileSource(tile.Provider{})
	cmd := m.SetLocation("london", 10)
	if cmd == nil {
		t.Fatal("expected the lookup Cmd")
	}
	msg, ok := cmd().(MapCoordinates)
	if !ok || msg.Err != nil || msg.Lat != 51.5 || msg.Lng != -0.12 || asked != "london" {
		t.Fatalf("lookup produced %#v (asked %q)", msg, asked)
	}
	m, _ = m.Update(msg)
	if lat, lng := m.Center(); lat != 51.5 || lng != -0.12 || m.Zoom() != 10 {
		t.Fatalf("center = %v,%v z%d", lat, lng, m.Zoom())
	}
	cmd = m.SetLocation("nowhere", 10)
	if msg := cmd().(MapCoordinates); !errors.Is(msg.Err, geocode.ErrNotFound) {
		t.Fatalf("lookup error = %v, want ErrNotFound", msg.Err)
	}
	if New(1, 1).geocoder == nil {
		t.Fatal("default geocoder should be set")
	}
	if m.SetLocation("", 5) != nil || m.Zoom() != 5 {
		t.Fatal("an empty query only sets the zoom")
	}
}
