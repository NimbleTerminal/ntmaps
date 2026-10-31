package ntmaps

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

type rejectNetwork struct{ calls int }

func (r *rejectNetwork) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return nil, fmt.Errorf("unexpected request: %s", req.URL)
}

func TestDefaultsDoNotAccessNetwork(t *testing.T) {
	transport := &rejectNetwork{}
	old := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = old })
	m := NewWithConfig(Config{Cols: 20, Rows: 10, DisableDiskCache: true})
	defer m.Close()
	if m.TileSource() != nil || m.geocoder != nil || m.prefetch != 0 {
		t.Fatal("network services must be opt-in")
	}
	drainMap(t, &m, m.renderMapCmd())
	if m.Image() == nil || m.pic.View() == "" {
		t.Fatal("no-source map must render a placeholder")
	}
	msg := m.SetLocation("London", 10)().(MapCoordinates)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "no geocoder configured") {
		t.Fatalf("lookup error = %v", msg.Err)
	}
	img, err := Render(context.Background(), RenderOptions{Width: 40, Height: 20, Zoom: 10, Loader: tile.NewLoader(tile.Options{})})
	if err != nil || img == nil {
		t.Fatalf("no-source render: %v", err)
	}
	if transport.calls != 0 {
		t.Fatalf("defaults made %d HTTP requests", transport.calls)
	}
}

func TestDiskCacheConfig(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		s := newTestTiles(t)
		dir := filepath.Join(t.TempDir(), "tiles")
		m := NewWithConfig(Config{TileCacheDir: dir, DisableDiskCache: disabled, TileCacheCap: -1, TileCacheDefaultTTL: -1, TileCacheDiskCap: 1 << 20})
		for i := 0; i < 2; i++ {
			if _, err := m.loader.Fetch(context.Background(), s.provider(), tile.ID{}); err != nil {
				t.Fatal(err)
			}
		}
		m.Close()
		if s.requests.Load() != 2 {
			t.Fatal("configured negative default TTL ignored")
		}
		_, err := os.Stat(dir)
		if disabled && !os.IsNotExist(err) {
			t.Fatal("disabled disk cache touched directory")
		}
		if !disabled && err != nil {
			t.Fatal("configured disk cache was not used", err)
		}
	}
	// A tile larger than the configured cap must not create the directory.
	s := newTestTiles(t)
	dir := filepath.Join(t.TempDir(), "tiles")
	m := NewWithConfig(Config{TileCacheDir: dir, TileCacheDiskCap: 1})
	defer m.Close()
	if _, err := m.loader.Fetch(context.Background(), s.provider(), tile.ID{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("disk cap was not passed to Loader")
	}
}
