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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tileServer serves a solid 256×256 PNG per tile and counts requests.
// Tiles whose path contains "/404/" return 404. When hold is non-nil the
// handler blocks until it is closed, signalling started first.
type tileServer struct {
	*httptest.Server
	requests  atomic.Int32
	userAgent atomic.Pointer[string]
	hold      chan struct{}
	started   chan struct{}
	canceled  chan struct{}
}

func newTileServer(t *testing.T) *tileServer {
	t.Helper()
	s := &tileServer{started: make(chan struct{}, 16), canceled: make(chan struct{}, 16)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		ua := r.Header.Get("User-Agent")
		s.userAgent.Store(&ua)
		if s.hold != nil {
			s.started <- struct{}{}
			select {
			case <-s.hold:
			case <-r.Context().Done():
				s.canceled <- struct{}{}
				return
			}
		}
		if strings.Contains(r.URL.Path, "/404/") {
			http.NotFound(w, r)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		for i := range img.Pix {
			img.Pix[i] = 0xff
		}
		img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *tileServer) provider() Provider {
	return Provider{Name: "test", URL: s.URL + "/{z}/{x}/{y}.png"}
}

func checkTile(t *testing.T, img image.Image) {
	t.Helper()
	if img == nil || img.Bounds().Dx() != 256 {
		t.Fatalf("unexpected tile %v", img)
	}
	if c := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA); c != (color.RGBA{1, 2, 3, 255}) {
		t.Fatalf("tile pixel = %v", c)
	}
}

func TestFetchCachesInMemory(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{})
	id := ID{3, 1, 2}
	img, err := l.Fetch(context.Background(), s.provider(), id)
	if err != nil {
		t.Fatal(err)
	}
	checkTile(t, img)
	if _, ok := l.Cached(s.provider(), id); !ok {
		t.Fatal("tile should be cached after Fetch")
	}
	if _, err := l.Fetch(context.Background(), s.provider(), id); err != nil {
		t.Fatal(err)
	}
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1", n)
	}
}

func TestFetchUsesDiskCache(t *testing.T) {
	s := newTileServer(t)
	dir := t.TempDir()
	id := ID{5, 6, 7}
	if _, err := NewLoader(Options{Dir: dir}).Fetch(context.Background(), s.provider(), id); err != nil {
		t.Fatal(err)
	}
	fresh := NewLoader(Options{Dir: dir})
	if _, ok := fresh.Cached(s.provider(), id); ok {
		t.Fatal("fresh loader should start with an empty memory cache")
	}
	img, err := fresh.Fetch(context.Background(), s.provider(), id)
	if err != nil {
		t.Fatal(err)
	}
	checkTile(t, img)
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1 (disk cache miss)", n)
	}
	if _, err := NewLoader(Options{Dir: filepath.Join(dir, "missing")}).Fetch(context.Background(), s.provider(), id); err != nil {
		t.Fatal(err)
	}
	if n := s.requests.Load(); n != 2 {
		t.Fatalf("server saw %d requests, want 2", n)
	}
}

func TestFetchDeduplicatesInFlight(t *testing.T) {
	s := newTileServer(t)
	s.hold = make(chan struct{})
	l := NewLoader(Options{})
	id := ID{1, 0, 0}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			img, err := l.Fetch(context.Background(), s.provider(), id)
			if err == nil {
				checkTile(t, img)
			}
			results[i] = err
		}(i)
	}
	<-s.started
	time.Sleep(20 * time.Millisecond)
	close(s.hold)
	wg.Wait()
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1", n)
	}
}

func TestCancelOneWaiterKeepsDownload(t *testing.T) {
	s := newTileServer(t)
	s.hold = make(chan struct{})
	l := NewLoader(Options{})
	id := ID{1, 1, 1}
	ctxA, cancelA := context.WithCancel(context.Background())
	errA := make(chan error, 1)
	go func() { _, err := l.Fetch(ctxA, s.provider(), id); errA <- err }()
	<-s.started
	imgB := make(chan image.Image, 1)
	go func() { img, _ := l.Fetch(context.Background(), s.provider(), id); imgB <- img }()
	time.Sleep(20 * time.Millisecond)
	cancelA()
	select {
	case err := <-errA:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("A returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter did not return")
	}
	select {
	case <-s.canceled:
		t.Fatal("download was canceled although a waiter remained")
	case <-time.After(50 * time.Millisecond):
	}
	close(s.hold)
	checkTile(t, <-imgB)
	if n := s.requests.Load(); n != 1 {
		t.Fatalf("server saw %d requests, want 1", n)
	}
}

func TestCancelLastWaiterCancelsDownload(t *testing.T) {
	s := newTileServer(t)
	s.hold = make(chan struct{})
	defer close(s.hold)
	l := NewLoader(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := l.Fetch(ctx, s.provider(), ID{2, 2, 2}); errc <- err }()
	<-s.started
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	select {
	case <-s.canceled:
	case <-time.After(time.Second):
		t.Fatal("download kept running with no waiters")
	}
}

func TestFetchNotFound(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{})
	_, err := l.Fetch(context.Background(), s.provider(), ID{404, 0, 0})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestFetchRejectsEmptyProvider(t *testing.T) {
	if _, err := NewLoader(Options{}).Fetch(context.Background(), Provider{}, ID{}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("got %v, want ErrNoSource", err)
	}
}

func TestFetchAllToleratesMissingTiles(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{Concurrency: 2})
	var got []ID
	var mu sync.Mutex
	err := l.FetchAll(context.Background(), s.provider(), []ID{{1, 0, 0}, {404, 0, 0}, {1, 1, 1}}, func(id ID, img image.Image) {
		checkTile(t, img)
		mu.Lock()
		got = append(got, id)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("onTile called for %v, want two tiles", got)
	}
	err = l.FetchAll(context.Background(), s.provider(), []ID{{404, 1, 0}, {404, 2, 0}}, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("all-missing FetchAll returned %v, want ErrNotFound", err)
	}
	if err := l.FetchAll(context.Background(), s.provider(), nil, nil); err != nil {
		t.Fatalf("empty FetchAll returned %v", err)
	}
}

func TestMemoryCapEvictsOldest(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{MemoryCap: 2})
	p := s.provider()
	for _, id := range []ID{{1, 0, 0}, {1, 1, 0}, {1, 0, 1}} {
		if _, err := l.Fetch(context.Background(), p, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := l.Cached(p, ID{1, 0, 0}); ok {
		t.Fatal("oldest tile should have been evicted")
	}
	if _, ok := l.Cached(p, ID{1, 0, 1}); !ok {
		t.Fatal("newest tile should be cached")
	}
	if _, ok := NewLoader(Options{MemoryCap: -1}).Cached(p, ID{}); ok {
		t.Fatal("disabled memory cache should never hit")
	}
}

func TestDefaultUserAgentNamesNtmaps(t *testing.T) {
	ua := DefaultUserAgent()
	if !strings.HasPrefix(ua, "ntmaps/") || !strings.Contains(ua, "github.com/NimbleTerminal/ntmaps") {
		t.Fatalf("unexpected user agent %q", ua)
	}
}

func TestConcurrencyLimitIsGlobal(t *testing.T) {
	s := newTileServer(t)
	s.hold = make(chan struct{})
	l := NewLoader(Options{Concurrency: 2})
	p := s.provider()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = l.Fetch(context.Background(), p, ID{Z: 9, X: i, Y: 0})
		}(i)
	}
	<-s.started
	<-s.started
	select {
	case <-s.started:
		t.Fatal("a third download started past the concurrency limit")
	case <-time.After(50 * time.Millisecond):
	}
	if n := s.requests.Load(); n != 2 {
		t.Fatalf("server saw %d requests, want 2", n)
	}
	close(s.hold)
	wg.Wait()
	if n := s.requests.Load(); n != 5 {
		t.Fatalf("server saw %d requests after release, want 5", n)
	}
}

// funcSource is a Source made of a function, as an offline file or an
// in-memory generator would be.
type funcSource struct {
	name  string
	calls atomic.Int32
	tile  func(ID) (image.Image, error)
}

func (f *funcSource) Info() Info { return Info{Name: f.name, Placeholder: color.Black} }
func (f *funcSource) Tile(ctx context.Context, id ID) (image.Image, error) {
	f.calls.Add(1)
	return f.tile(id)
}

func TestLoaderCachesAnySource(t *testing.T) {
	src := &funcSource{name: "gen", tile: func(id ID) (image.Image, error) {
		if id.Z == 404 {
			return nil, ErrNotFound
		}
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		return img, nil
	}}
	l := NewLoader(Options{Dir: t.TempDir()})
	img, err := l.Fetch(context.Background(), src, ID{Z: 3, X: 1, Y: 1})
	if err != nil {
		t.Fatal(err)
	}
	checkTile(t, img)
	if _, err := l.Fetch(context.Background(), src, ID{Z: 3, X: 1, Y: 1}); err != nil || src.calls.Load() != 1 {
		t.Fatalf("second fetch: err=%v calls=%d, want cached", err, src.calls.Load())
	}
	if _, err := l.Fetch(context.Background(), src, ID{Z: 404}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if Fetchable(src) != true || Fetchable(nil) || Fetchable(Provider{}) || !Fetchable(OpenStreetMap()) {
		t.Fatal("Fetchable misjudged a source")
	}
	if _, err := l.Fetch(context.Background(), nil, ID{}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("nil source: got %v, want ErrNoSource", err)
	}
}
