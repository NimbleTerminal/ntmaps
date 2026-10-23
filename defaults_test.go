package ntmaps

import (
	"context"
	"fmt"
	"net/http"
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
