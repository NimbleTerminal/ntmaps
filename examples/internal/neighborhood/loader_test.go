package neighborhood

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"nimbleterminal.dev/ntmaps/examples/internal/cityview"
	"nimbleterminal.dev/ntmaps/internal/city"
)

func dataset(t *testing.T) city.Dataset {
	t.Helper()
	d, err := cityview.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestCacheReusePersistenceAndRefresh(t *testing.T) {
	d := dataset(t)
	dir := t.TempDir()
	var calls atomic.Int32
	fetch := func(context.Context, city.Bounds) (city.Dataset, error) { calls.Add(1); return d, nil }
	center := d.Bounds.Center()
	l := &Loader{Dir: dir, Fetch: fetch}
	a, err := l.Load(context.Background(), center, 200, false)
	if err != nil || a.Cached {
		t.Fatalf("first load %v", err)
	}
	b, err := l.Load(context.Background(), center, 200, false)
	if err != nil || !b.Cached || calls.Load() != 1 {
		t.Fatal("memory cache missed")
	}
	if &a.Scene.Buildings[0].Mesh.Vertices[0] != &b.Scene.Buildings[0].Mesh.Vertices[0] {
		t.Fatal("cached geometry recompiled")
	}
	disk := &Loader{Dir: dir}
	c, err := disk.Load(context.Background(), center, 200, false)
	if err != nil || !c.Cached || len(c.Scene.Buildings) != len(a.Scene.Buildings) {
		t.Fatalf("offline disk load: %v", err)
	}
	if _, err = l.Load(context.Background(), center, 200, true); err != nil || calls.Load() != 2 {
		t.Fatal("explicit refresh did not fetch")
	}
	// Invalid/truncated data must not be used as a valid neighborhood.
	if err = os.WriteFile(filepath.Join(dir, filename(Key(center, 200))), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	l2 := &Loader{Dir: dir, Fetch: fetch}
	if _, err = l2.Load(context.Background(), center, 200, false); err != nil || calls.Load() != 3 {
		t.Fatal("corrupt cache did not refetch")
	}
}
func TestSeedExpiryBoundsAndCanceledWork(t *testing.T) {
	d := dataset(t)
	center := d.Bounds.Center()
	l := &Loader{}
	if err := l.Seed(center, 200, d); err != nil {
		t.Fatal(err)
	}
	if result, err := l.Load(context.Background(), center, 200, false); err != nil || !result.Cached {
		t.Fatal("offline seed missed")
	}
	if _, err := l.Load(context.Background(), city.Coordinate{Lat: 10, Lon: 10}, 200, false); err == nil {
		t.Fatal("uncached offline location succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Load(ctx, center, 200, false); err == nil {
		t.Fatal("canceled load succeeded")
	}
	for i := 0; i < 12; i++ {
		l.put(string(rune('a'+i)), Result{}, 0, time.Now().Add(time.Hour))
	}
	if len(l.entries) > maxEntries {
		t.Fatal("cache grew past count limit")
	}
	key := Key(center, 200)
	l.put(key, Result{}, 0, time.Now().Add(-time.Hour))
	if _, err := l.Load(context.Background(), center, 200, false); err == nil {
		t.Fatal("expired memory entry used")
	}
}

func TestLoadReusesSeededNeighborhoodCoveringTheCenter(t *testing.T) {
	d, err := cityview.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	l := &Loader{}
	center := d.Bounds.Center()
	if err := l.Seed(center, 200, d); err != nil {
		t.Fatal(err)
	}
	nearby := city.Coordinate{Lat: center.Lat + 0.0002, Lon: center.Lon - 0.0003}
	r, err := l.Load(context.Background(), nearby, 200, false)
	if err != nil || !r.Cached || len(r.Scene.Buildings) == 0 {
		t.Fatalf("nearby center should reuse the seeded neighborhood: err=%v cached=%v", err, r.Cached)
	}
	outside := city.Coordinate{Lat: d.Bounds.North + 0.01, Lon: center.Lon}
	if _, err := l.Load(context.Background(), outside, 200, false); err == nil {
		t.Fatal("a center outside every cached neighborhood must not be served offline")
	}
}
