package main

import (
	"image"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"nimbleterminal.dev/ntmaps/examples/internal/places"
	picture "nimbleterminal.dev/ntpicture"
)

func TestRendererTogglePreservesMapAndCamera(t *testing.T) {
	m := newModel([]places.Place{{Name: "Test", Lat: 40, Lon: 10, Zoom: 10}}, picture.KittyMediumDirect, chart.Software)
	defer m.chart.Close()
	defer m.source.Close()
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	camera := m.chart.Camera()
	lat, lng := m.source.Center()
	for _, want := range []chart.RenderMode{chart.WebGPU, chart.Software, chart.WebGPU} {
		m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
		if m.chart.RequestedRenderMode() != want || m.chart.RenderMode() != want {
			t.Fatalf("toggle did not select %v", want)
		}
		if m.chart.Camera() != camera {
			t.Fatal("renderer toggle reset the camera")
		}
		if a, b := m.source.Center(); a != lat || b != lng || m.source.Zoom() != 10 {
			t.Fatal("renderer toggle moved the map")
		}
		if !strings.Contains(m.View().Content, want.String()+" · ") {
			t.Fatal("status does not identify the selected renderer")
		}
	}
}

func TestCameraAndMapControlsAreIndependent(t *testing.T) {
	m := newModel([]places.Place{{Name: "Test", Lat: 40, Lon: 10, Zoom: 10}}, picture.KittyMediumDirect, chart.Software)
	defer m.chart.Close()
	defer m.source.Close()
	lat, lng := m.source.Center()
	camera := m.chart.Camera()
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.chart.Camera().Beta == camera.Beta {
		t.Fatal("right arrow did not rotate the camera")
	}
	if a, b := m.source.Center(); a != lat || b != lng {
		t.Fatal("camera rotation moved the map")
	}
	camera = m.chart.Camera()
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if _, b := m.source.Center(); b <= lng {
		t.Fatal("D did not pan the map east")
	}
	if m.chart.Camera() != camera {
		t.Fatal("map panning moved the camera")
	}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.source.Zoom() != 11 {
		t.Fatal("map zoom key did not reach mapview")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.chart.Camera() != camera {
		t.Fatal("resize reset the camera")
	}
}

type captureRenderer struct{ frame *chart.Frame }

func (r *captureRenderer) Render(f chart.Frame) (image.Image, error) {
	r.frame = &f
	return image.NewRGBA(image.Rect(0, 0, f.Width, f.Height)), nil
}
func (*captureRenderer) Close() error { return nil }

func TestMapSurfacePreservesAspectAndNorthUpTexture(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	texture, err := chart.NewTexture(img)
	if err != nil {
		t.Fatal(err)
	}
	r := &captureRenderer{}
	c := chart.New(40, 20, chart.WithRenderer(r))
	defer c.Close()
	c.SetSurface("map", mapSurface(texture, img.Bounds()))
	if c.Err() != nil {
		t.Fatal(c.Err())
	}
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil || r.frame != nil {
			return
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, child := range batch {
				run(child)
			}
		}
	}
	run(c.Init())
	if r.frame == nil || len(r.frame.Geometry) != 1 {
		t.Fatal("surface was not rendered")
	}
	g := r.frame.Geometry[0]
	if g.Material.Texture != texture || !g.Material.Unlit {
		t.Fatal("map texture lost its original colors")
	}
	if len(g.Vertices) != 4 || len(g.Indices) != 6 {
		t.Fatal("expected a flat two-triangle surface")
	}
	if g.Bounds.Max.X-g.Bounds.Min.X != 2*(g.Bounds.Max.Y-g.Bounds.Min.Y) {
		t.Fatal("map aspect ratio changed")
	}
	for i, v := range g.Vertices {
		if v.Position.Z != 0 {
			t.Fatal("flat map acquired elevation")
		}
		if v.Position.Y == g.Bounds.Max.Y && g.UV[i].V != 0 {
			t.Fatal("north must use the top of the texture")
		}
	}
}

func TestMouseControlsOnlyVisibleChart(t *testing.T) {
	m := newModel([]places.Place{{Name: "Test", Lat: 40, Lon: 10, Zoom: 10}}, picture.KittyMediumDirect, chart.Wireframe)
	defer m.chart.Close()
	defer m.source.Close()
	defer m.session.Close()
	m.View()
	lat, lon := m.source.Center()
	zoom := m.source.Zoom()
	camera := m.chart.Camera()
	for _, button := range []tea.MouseButton{tea.MouseWheelLeft, tea.MouseWheelRight} {
		m.Update(tea.MouseWheelMsg{X: 20, Y: 10, Button: button})
		if m.chart.Camera() != camera {
			t.Fatal("horizontal wheel changed camera")
		}
	}
	m.Update(tea.MouseWheelMsg{X: 20, Y: 10, Button: tea.MouseWheelUp})
	if m.chart.Camera().Distance >= camera.Distance {
		t.Fatal("wheel up did not move camera closer")
	}
	m.Update(tea.MouseWheelMsg{X: 20, Y: 10, Button: tea.MouseWheelDown})
	if m.chart.Camera().Distance != camera.Distance {
		t.Fatal("wheel down did not restore distance")
	}
	m.Update(tea.MouseClickMsg{X: 20, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 24, Y: 12, Button: tea.MouseLeft})
	if m.chart.Camera().Beta == camera.Beta {
		t.Fatal("drag did not orbit chart")
	}
	m.Update(tea.MouseReleaseMsg{X: 24, Y: 12, Button: tea.MouseLeft})
	if a, b := m.source.Center(); a != lat || b != lon || m.source.Zoom() != zoom || m.source.Dragging() {
		t.Fatal("chart mouse input changed hidden map")
	}
}
