package tile

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestFirstOfFallsThroughOnNotFound(t *testing.T) {
	local := &funcSource{name: "local", tile: func(id ID) (image.Image, error) {
		if id.Z != 12 {
			return nil, ErrNotFound
		}
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
		return img, nil
	}}
	s := newTileServer(t)
	online := s.provider()
	online.Attribution = "Online (c)"
	src := FirstOf(nil, local, Provider{}, online)
	info := src.Info()
	if info.Name != "local+test" || info.Attribution != "Online (c)" || info.Max() != 19 {
		t.Fatalf("info = %+v", info)
	}
	l := NewLoader(Options{Dir: t.TempDir()})
	if _, err := l.Fetch(context.Background(), src, ID{Z: 12, X: 1, Y: 1}); err != nil || local.calls.Load() != 1 || s.requests.Load() != 0 {
		t.Fatalf("local tile: err=%v local=%d online=%d", err, local.calls.Load(), s.requests.Load())
	}
	img, err := l.Fetch(context.Background(), src, ID{Z: 5, X: 1, Y: 1})
	if err != nil || s.requests.Load() != 1 {
		t.Fatalf("online tile: err=%v online=%d", err, s.requests.Load())
	}
	checkTile(t, img)
	if FirstOf(nil, Provider{}) != nil {
		t.Fatal("no fetchable sources should give nil")
	}
	if got := FirstOf(online); got.Info().Name != "test" {
		t.Fatal("a single source is returned as is")
	}
	failing := &funcSource{name: "bad", tile: func(ID) (image.Image, error) { return nil, errors.New("disk on fire") }}
	if _, err := FirstOf(failing, online).Tile(context.Background(), ID{Z: 1}); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a real error must not fall through: %v", err)
	}
}
