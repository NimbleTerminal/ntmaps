package geocode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newServer(t *testing.T, body string) (*httptest.Server, *atomic.Pointer[http.Request]) {
	t.Helper()
	var last atomic.Pointer[http.Request]
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last.Store(r.Clone(context.Background()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &last
}

func TestNominatimParsesFirstHit(t *testing.T) {
	s, last := newServer(t, `[{"lat":"40.7415","lon":"-73.989","display_name":"Flatiron Building"},{"lat":"0","lon":"0"}]`)
	n := &Nominatim{Endpoint: s.URL + "/search", UserAgent: "ntmaps-test/1.0", MinInterval: time.Millisecond}
	got, err := n.Geocode(context.Background(), "flatiron building")
	if err != nil {
		t.Fatal(err)
	}
	if got.Lat != 40.7415 || got.Lng != -73.989 || got.Name != "Flatiron Building" {
		t.Fatalf("result = %+v", got)
	}
	r := last.Load()
	if r.Header.Get("User-Agent") != "ntmaps-test/1.0" {
		t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
	}
	if q := r.URL.Query(); q.Get("q") != "flatiron building" || q.Get("format") != "json" || q.Get("limit") != "1" {
		t.Fatalf("query = %v", q)
	}
}

func TestNominatimNotFoundAndErrors(t *testing.T) {
	s, _ := newServer(t, `[]`)
	n := &Nominatim{Endpoint: s.URL, MinInterval: time.Millisecond}
	if _, err := n.Geocode(context.Background(), "nowhere"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	bad, _ := newServer(t, `not json`)
	n = &Nominatim{Endpoint: bad.URL, MinInterval: time.Millisecond}
	if _, err := n.Geocode(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("got %v, want decode error", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "over quota", http.StatusTooManyRequests)
	}))
	t.Cleanup(down.Close)
	n = &Nominatim{Endpoint: down.URL, MinInterval: time.Millisecond}
	if _, err := n.Geocode(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("got %v, want status error", err)
	}
}

func TestNominatimPacesRequests(t *testing.T) {
	s, _ := newServer(t, `[{"lat":"1","lon":"2"}]`)
	n := &Nominatim{Endpoint: s.URL, MinInterval: 60 * time.Millisecond}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := n.Geocode(context.Background(), "x"); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Fatalf("three requests took %v, want at least two intervals", elapsed)
	}
	// A canceled context does not wait for the slot.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := n.Geocode(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestFuncAdapter(t *testing.T) {
	g := Func(func(ctx context.Context, q string) (Result, error) { return Result{Lat: 1, Lng: 2, Name: q}, nil })
	got, err := g.Geocode(context.Background(), "here")
	if err != nil || got != (Result{Lat: 1, Lng: 2, Name: "here"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}
