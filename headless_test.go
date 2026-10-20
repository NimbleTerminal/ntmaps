package ntmaps

import (
	"context"
	"image/color"
	"testing"

	"nimbleterminal.dev/ntmaps/tile"
)

func TestRenderComposesAFullImage(t *testing.T) {
	s := newTestTiles(t)
	credited := s.provider()
	credited.Attribution = "Test tiles"
	img, err := Render(context.Background(), RenderOptions{
		Source: credited, Loader: tile.NewLoader(tile.Options{}),
		Lat: 40.7415, Lng: -73.989, Zoom: 12, Width: 300, Height: 120,
		Markers: []Marker{{Lat: 40.7415, Lng: -73.989}},
		Paths:   []Path{{Points: []Point{{Lat: 40.74, Lng: -74}, {Lat: 40.743, Lng: -73.98}}, Color: color.RGBA{A: 255}, Width: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 120 {
		t.Fatalf("size = %v", b)
	}
	if got := centerPixel(img); got == tileGreen {
		t.Fatal("marker pin should cover the center")
	}
	if got := color.RGBAModel.Convert(img.At(10, 10)).(color.RGBA); got != tileGreen {
		t.Fatalf("corner pixel = %v, want tile color", got)
	}
	if got := color.RGBAModel.Convert(img.At(150, 119)).(color.RGBA); got == tileGreen {
		t.Fatal("bottom row should carry the attribution strip")
	}
	plain, err := Render(context.Background(), RenderOptions{Source: s.provider(), Loader: tile.NewLoader(tile.Options{}), Zoom: 12, Width: 64, Height: 32, NoAttribution: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := color.RGBAModel.Convert(plain.At(32, 31)).(color.RGBA); got != tileGreen {
		t.Fatalf("NoAttribution bottom row = %v, want tile color", got)
	}
}

func TestRenderValidatesAndReportsFailure(t *testing.T) {
	if _, err := Render(context.Background(), RenderOptions{Width: 0, Height: 10}); err == nil {
		t.Fatal("expected a size error")
	}
	shallow := tile.Provider{Name: "shallow", URL: "x/{z}/{x}/{y}", MaxZoom: 5}
	if _, err := Render(context.Background(), RenderOptions{Source: shallow, Zoom: 9, Width: 10, Height: 10}); err == nil {
		t.Fatal("expected a zoom error")
	}
	img, err := Render(context.Background(), RenderOptions{Source: tile.Provider{Placeholder: color.RGBA{7, 7, 7, 255}}, Zoom: 3, Width: 20, Height: 20})
	if err != nil || color.RGBAModel.Convert(img.At(5, 5)).(color.RGBA) != (color.RGBA{7, 7, 7, 255}) {
		t.Fatalf("empty source: img=%v err=%v, want placeholder without error", img, err)
	}
	s := newTestTiles(t)
	s.fail.Store(true)
	if _, err := Render(context.Background(), RenderOptions{Source: s.provider(), Loader: tile.NewLoader(tile.Options{}), Zoom: 12, Width: 40, Height: 40}); err == nil {
		t.Fatal("expected an error when no tile loads")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.fail.Store(false)
	if _, err := Render(ctx, RenderOptions{Source: s.provider(), Loader: tile.NewLoader(tile.Options{}), Zoom: 12, Width: 40, Height: 40}); err == nil {
		t.Fatal("expected a canceled render to fail")
	}
}
