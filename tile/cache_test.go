package tile

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func cachePNG(t *testing.T, red uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: red, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func fetchCache(t *testing.T, l *Loader, p Source, id ID) image.Image {
	t.Helper()
	img, err := l.Fetch(context.Background(), p, id)
	if err != nil {
		t.Fatal(err)
	}
	return img
}
func expireCache(t *testing.T, l *Loader, p Source, id ID) {
	t.Helper()
	path := l.diskPath(p, id)
	m, b := readCache(path)
	if len(b) == 0 {
		t.Fatal("missing cache")
	}
	m.Expires = time.Now().Add(-time.Hour)
	l.writeCache(path, m, b)
}

func TestCacheRevalidation(t *testing.T) {
	for _, status := range []int{304, 200, 500, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			original, replacement := cachePNG(t, 10), cachePNG(t, 20)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				w.Header().Set("Cache-Control", "max-age=3600")
				w.Header().Set("ETag", `"one"`)
				w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
				if n == 1 {
					w.Write(original)
					return
				}
				if r.Header.Get("If-None-Match") != `"one"` || r.Header.Get("If-Modified-Since") == "" {
					t.Error("missing validators")
				}
				w.WriteHeader(status)
				if status == 200 {
					w.Write(replacement)
				}
			}))
			defer srv.Close()
			p := Provider{Name: "test", URL: srv.URL}
			l := NewLoader(Options{Dir: t.TempDir(), MemoryCap: -1})
			id := ID{}
			fetchCache(t, l, p, id)
			fetchCache(t, l, p, id)
			if calls.Load() != 1 {
				t.Fatal("fresh cache hit made a request")
			}
			expireCache(t, l, p, id)
			before, _ := os.ReadFile(l.diskPath(p, id))
			img, err := l.Fetch(context.Background(), p, id)
			if status == 404 {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("404: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := uint8(10)
			if status == 200 {
				want = 20
			}
			if got := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA).R; got != want {
				t.Fatalf("pixel %d want %d", got, want)
			}
			after, _ := os.ReadFile(l.diskPath(p, id))
			if status == 500 {
				if !bytes.Equal(before, after) {
					t.Fatal("5xx rewrote cache")
				}
				return
			}
			meta, _ := readCache(l.diskPath(p, id))
			if time.Until(meta.Expires) < 59*time.Minute {
				t.Fatal("freshness not extended")
			}
			fetchCache(t, l, p, id)
			if calls.Load() != 2 {
				t.Fatal("refreshed tile made a request")
			}
		})
	}
}

func TestCacheNetworkFailure(t *testing.T) {
	data := cachePNG(t, 42)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	p := Provider{Name: "test", URL: srv.URL}
	l := NewLoader(Options{Dir: t.TempDir(), MemoryCap: -1})
	id := ID{}
	fetchCache(t, l, p, id)
	expireCache(t, l, p, id)
	srv.Close()
	path := l.diskPath(p, id)
	before, _ := os.ReadFile(path)
	stat, _ := os.Stat(path)
	fetchCache(t, l, p, id)
	after, _ := os.ReadFile(path)
	statAfter, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !stat.ModTime().Equal(statAfter.ModTime()) {
		t.Fatal("failed revalidation changed cache")
	}
}

func TestCachePolicies(t *testing.T) {
	for _, policy := range []string{"", "no-store", "no-cache", "max-age=0", "s-maxage=0"} {
		t.Run(policy, func(t *testing.T) {
			data := cachePNG(t, 1)
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if policy != "" {
					w.Header().Set("Cache-Control", policy)
				}
				w.Write(data)
			}))
			defer srv.Close()
			p := Provider{Name: "test", URL: srv.URL}
			dir := filepath.Join(t.TempDir(), "cache")
			l := NewLoader(Options{Dir: dir})
			id := ID{}
			fetchCache(t, l, p, id)
			fetchCache(t, l, p, id)
			meta, b := readCache(l.diskPath(p, id))
			switch policy {
			case "no-store":
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatal("no-store created cache directory")
				}
				if calls.Load() != 2 {
					t.Fatal("no-store retained in memory")
				}
			case "no-cache", "max-age=0":
				if len(b) == 0 || calls.Load() != 2 {
					t.Fatal("must store and revalidate each use")
				}
			default:
				if calls.Load() != 1 || time.Until(meta.Expires) < 7*24*time.Hour-time.Minute {
					t.Fatal("expected seven-day default")
				}
			}
		})
	}
}

func TestFreshness(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name, cc, expires, age string
		want                   time.Duration
		store                  bool
	}{
		{"default", "", "", "", 7 * 24 * time.Hour, true},
		{"private", "s-maxage=0", "", "", 7 * 24 * time.Hour, true},
		{"age", "max-age=600", "", "120", 480 * time.Second, true},
		{"expires", "", now.Add(time.Hour).Format(http.TimeFormat), "", time.Hour, true},
		{"precedence", "max-age=60", now.Add(time.Hour).Format(http.TimeFormat), "", time.Minute, true},
		{"quoted", `max-age="60"`, "", "", time.Minute, true},
		{"no-cache", "no-cache, max-age=600", "", "", 0, true},
		{"no-store", "no-store", "", "", 7 * 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("Date", now.Format(http.TimeFormat))
			h.Set("Cache-Control", tc.cc)
			h.Set("Expires", tc.expires)
			h.Set("Age", tc.age)
			exp, noStore := freshness(h, now, 7*24*time.Hour)
			if exp.Sub(now) != tc.want || noStore == tc.store {
				t.Fatalf("got %v noStore=%v", exp.Sub(now), noStore)
			}
		})
	}
	exp, _ := freshness(nil, now, time.Hour)
	if exp.Sub(now) != time.Hour {
		t.Fatal("custom TTL ignored")
	}
}

func TestCacheLRUAndSharedLoaders(t *testing.T) {
	data := cachePNG(t, 1)
	p := Provider{Name: "test", URL: "http://unused.invalid"}
	meta := cacheMeta{Expires: time.Now().Add(time.Hour)}
	dir := t.TempDir()
	l := NewLoader(Options{Dir: dir, DiskCap: -1})
	path := func(i int) string { return l.diskPath(p, ID{X: i}) }
	l.writeCache(path(0), meta, data)
	st, _ := os.Stat(path(0))
	size := st.Size()
	l = NewLoader(Options{Dir: dir, DiskCap: size * 3, MemoryCap: -1})
	l.writeCache(path(1), meta, data)
	l.writeCache(path(2), meta, data)
	for i := 0; i < 3; i++ {
		old := time.Now().Add(time.Duration(i-10) * time.Hour)
		os.Chtimes(path(i), old, old)
	}
	// A real disk read makes tile zero newest.
	fetchCache(t, l, p, ID{})
	l.writeCache(path(3), meta, data)
	if _, err := os.Stat(path(0)); err != nil {
		t.Fatal("read did not protect tile zero")
	}
	if _, err := os.Stat(path(1)); !os.IsNotExist(err) {
		t.Fatal("oldest tile survived eviction")
	}

	shared := t.TempDir()
	cap := size * 20
	loaders := []*Loader{NewLoader(Options{Dir: shared, DiskCap: cap}), NewLoader(Options{Dir: shared, DiskCap: cap})}
	var wg sync.WaitGroup
	for n, loader := range loaders {
		wg.Add(1)
		go func(n int, l *Loader) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				path := l.diskPath(p, ID{X: i + n*100})
				l.writeCache(path, meta, data)
				_, b := readCache(path)
				if len(b) > 0 && !bytes.Equal(b, data) {
					t.Error("corrupt concurrent read")
				}
			}
		}(n, loader)
	}
	wg.Wait()
	var total int64
	filepath.WalkDir(shared, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			st, e := d.Info()
			if e == nil {
				total += st.Size()
			}
		}
		return nil
	})
	if total > cap+size*2 {
		t.Fatalf("shared total %d exceeds cap %d", total, cap)
	}
}

func TestCacheSafety(t *testing.T) {
	s := newTileServer(t)
	for _, name := range []string{"../escape", "", "a/b", `a\b`, ".."} {
		p := s.provider()
		p.Name = name
		dir := t.TempDir()
		l := NewLoader(Options{Dir: dir, MemoryCap: -1})
		fetchCache(t, l, p, ID{})
		rel, err := filepath.Rel(dir, l.diskPath(p, ID{}))
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("unsafe path %s", rel)
		}
	}
	dir := t.TempDir()
	l := NewLoader(Options{Dir: dir})
	path := l.diskPath(s.provider(), ID{})
	if err := writeFileAtomic(path, append([]byte(cacheMagic), bytes.Repeat([]byte("x"), maxCacheMeta+2)...)); err != nil {
		t.Fatal(err)
	}
	if _, b := readCache(path); len(b) > 0 {
		t.Fatal("accepted oversized metadata")
	}
	l.writeCache(path, cacheMeta{Expires: time.Now().Add(time.Hour)}, cachePNG(t, 1))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(make([]byte, maxTileBytes))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, b := readCache(path); len(b) > 0 {
		t.Fatal("accepted oversized disk tile")
	}
}

func TestCacheDisabledAndLocal(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{MemoryCap: -1})
	fetchCache(t, l, s.provider(), ID{})
	fetchCache(t, l, s.provider(), ID{})
	if l.disk.entries != nil || s.requests.Load() != 2 {
		t.Fatal("disabled disk cache was used")
	}
	dir := filepath.Join(t.TempDir(), "cache")
	local := localDataSource{data: cachePNG(t, 1)}
	fetchCache(t, NewLoader(Options{Dir: dir}), local, ID{})
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("local source wrote disk cache")
	}
}

type localDataSource struct{ data []byte }

func (s localDataSource) Info() Info                                    { return Info{Name: "local"} }
func (s localDataSource) Tile(context.Context, ID) (image.Image, error) { return Decode(s.data) }
func (s localDataSource) TileData(context.Context, ID) ([]byte, error)  { return s.data, nil }
func (s localDataSource) DiskCacheable() bool                           { return false }

func TestCacheFirstOfPreservesPolicy(t *testing.T) {
	data := cachePNG(t, 1)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	}))
	defer srv.Close()
	missing := &funcSource{name: "missing", tile: func(ID) (image.Image, error) { return nil, ErrNotFound }}
	src := FirstOf(missing, Provider{Name: "online", URL: srv.URL})
	dir := filepath.Join(t.TempDir(), "tiles")
	l := NewLoader(Options{Dir: dir})
	fetchCache(t, l, src, ID{})
	fetchCache(t, l, src, ID{})
	if calls.Load() != 2 {
		t.Fatal("FirstOf lost no-store")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("FirstOf stored no-store")
	}
}

func TestCacheLegacyDataSource(t *testing.T) {
	data := cachePNG(t, 3)
	src := legacyDataSource{data: data}
	dir := t.TempDir()
	l := NewLoader(Options{Dir: dir, MemoryCap: -1})
	fetchCache(t, l, src, ID{})
	meta, got := readCache(l.diskPath(src, ID{}))
	if !bytes.Equal(got, data) || time.Until(meta.Expires) < 6*24*time.Hour {
		t.Fatal("legacy DataSource did not get default freshness")
	}
	fetchCache(t, l, src, ID{})
}

type legacyDataSource struct{ data []byte }

func (s legacyDataSource) Info() Info                                    { return Info{Name: "legacy"} }
func (s legacyDataSource) Tile(context.Context, ID) (image.Image, error) { return Decode(s.data) }
func (s legacyDataSource) TileData(context.Context, ID) ([]byte, error)  { return s.data, nil }

func TestCache304WithoutHeadersAndNoStoreTransition(t *testing.T) {
	for _, noStore := range []bool{false, true} {
		data := cachePNG(t, 1)
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Cache-Control", "max-age=3600")
				w.Header().Set("ETag", `"v1"`)
				w.Write(data)
				return
			}
			if noStore {
				w.Header().Set("Cache-Control", "no-store")
			}
			w.WriteHeader(304)
		}))
		p := Provider{Name: "test", URL: srv.URL}
		l := NewLoader(Options{Dir: t.TempDir(), MemoryCap: -1})
		fetchCache(t, l, p, ID{})
		expireCache(t, l, p, ID{})
		fetchCache(t, l, p, ID{})
		path := l.diskPath(p, ID{})
		if noStore {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("no-store retained old entry")
			}
		} else {
			m, b := readCache(path)
			if len(b) == 0 || time.Until(m.Expires) < 59*time.Minute || m.Header.Get("ETag") != `"v1"` {
				t.Fatal("304 lost original metadata")
			}
		}
		srv.Close()
	}
}
