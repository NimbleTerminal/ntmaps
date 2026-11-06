package tile

import (
	"image"
	"math"
	"net/http"
	"testing"
	"time"
)

// FuzzFreshness feeds arbitrary cache headers to the freshness rule. A
// tile must never expire before the moment it was received, whatever the
// server said, and no header may make the arithmetic panic.
func FuzzFreshness(f *testing.F) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	stamp := now.Add(-time.Minute).Format(http.TimeFormat)
	f.Add("max-age=60", "", stamp, "")
	f.Add("no-store", "", "", "")
	f.Add("no-cache, max-age=600", "", stamp, "30")
	f.Add("", now.Add(time.Hour).Format(http.TimeFormat), stamp, "")
	f.Add(`private, max-age="9223372036854775807"`, "", "", "9999999999999999999")
	f.Add("max-age=-5, max-age", "Thu, 01 Jan 1970 00:00:00 GMT", "garbage", "-1")
	f.Fuzz(func(t *testing.T, cacheControl, expires, date, age string) {
		h := http.Header{}
		for key, value := range map[string]string{"Cache-Control": cacheControl, "Expires": expires, "Date": date, "Age": age} {
			if value != "" {
				h.Set(key, value)
			}
		}
		for _, fallback := range []time.Duration{7 * 24 * time.Hour, 0, -time.Hour} {
			exp, _ := freshness(h, now, fallback)
			if exp.Before(now) {
				t.Fatalf("headers %v with fallback %v expire at %v, before %v", h, fallback, exp, now)
			}
		}
	})
}

// FuzzCover covers a view with tiles for arbitrary centers and sizes. Every
// column must wrap into the world, InWorld must match the row, and any
// view with a finite center must be tiled with no gap or overlap.
func FuzzCover(f *testing.F) {
	f.Add(1234.5, 678.9, 80, 24, 12, 256)
	f.Add(-0.5, 1e9, 1, 1, 0, 16)
	f.Add(math.Inf(1), math.NaN(), 300, 200, 30, 512)
	f.Add(1e300, -1e300, 64, 64, 31, 1024)
	f.Add(0.0, 0.0, 8, 8, 40, 256)
	f.Fuzz(func(t *testing.T, cx, cy float64, w, h, zoom, tileSize int) {
		// Bound the output: views up to 1024 pixels of tiles at least 16
		// pixels wide make at most 66×66 placements.
		w, h = min(w, 1024), min(h, 1024)
		zoom, tileSize = min(zoom, MaxCoverZoom+8), min(tileSize, 1024)
		if tileSize > 0 {
			tileSize = max(tileSize, 16)
		}
		out := Cover(cx, cy, w, h, zoom, tileSize)
		if w <= 0 || h <= 0 || tileSize <= 0 || zoom < 0 || zoom > MaxCoverZoom ||
			math.IsNaN(cx) || math.IsNaN(cy) || math.IsInf(cx, 0) || math.IsInf(cy, 0) {
			if out != nil {
				t.Fatalf("placements for a view that covers nothing: %v", out)
			}
			return
		}
		n := 1 << uint(zoom)
		for _, p := range out {
			if p.ID.Z != zoom || p.ID.X < 0 || p.ID.X >= n {
				t.Fatalf("placement %+v outside zoom %d's %d columns", p, zoom, n)
			}
			if p.InWorld != (p.ID.Y >= 0 && p.ID.Y < n) {
				t.Fatalf("placement %+v misreports InWorld", p)
			}
		}
		if len(out) == 0 {
			t.Fatal("no placements for a view")
		}
		first, last := out[0].Offset, out[len(out)-1].Offset
		if first.X > 0 || first.X <= -tileSize || first.Y > 0 || first.Y <= -tileSize {
			t.Fatalf("first placement at %v leaves the top-left corner bare", first)
		}
		if last.X+tileSize < w || last.Y+tileSize < h || last.X >= w || last.Y >= h {
			t.Fatalf("last placement at %v misses the bottom-right corner of %d×%d", last, w, h)
		}
		cols := (last.X-first.X)/tileSize + 1
		rows := (last.Y-first.Y)/tileSize + 1
		if cols*rows != len(out) {
			t.Fatalf("%d placements for %d×%d tiles", len(out), cols, rows)
		}
		for i, p := range out {
			want := first.Add(image.Pt((i%cols)*tileSize, (i/cols)*tileSize))
			if p.Offset != want {
				t.Fatalf("placement %d at %v, want %v", i, p.Offset, want)
			}
		}
	})
}
