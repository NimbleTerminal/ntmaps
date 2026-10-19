package cityview

import (
	"bytes"
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts-osm/internal/city"
	chart "github.com/NimbleMarkets/ntcharts3d"
	picture "nimbleterminal.dev/ntpicture"
)

func fixture(t *testing.T) (city.Dataset, city.Scene) {
	t.Helper()
	d, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	s, err := city.Build(d, city.DefaultHeightPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Buildings) < 50 || s.Stats.Recorded == 0 {
		t.Fatal("bundled neighborhood lost building/height data")
	}
	return d, s
}
func TestBundledSceneNavigationAndRendering(t *testing.T) {
	d, s := fixture(t)
	m, err := New(d, s, chart.Software, picture.KittyMediumDirect)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Chart.Close()
	snapshot := func() *image.RGBA {
		t.Helper()
		img, mode, err := m.Chart.Snapshot(320, 200)
		if err != nil || mode != chart.Software {
			t.Fatalf("snapshot: %v %v", mode, err)
		}
		return img.(*image.RGBA)
	}
	before := snapshot()
	held := bytes.Clone(before.Pix)
	camera := m.Chart.Camera()
	revision := len(m.Scene.Buildings[0].Mesh.Vertices)
	m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if m.Chart.Camera().Target == camera.Target {
		t.Fatal("movement did not translate camera")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.Chart.Camera().Beta == camera.Beta {
		t.Fatal("look did not rotate camera")
	}
	after := snapshot()
	if bytes.Equal(held, after.Pix) {
		t.Fatal("navigation did not change rendered city")
	}
	if !bytes.Equal(held, before.Pix) {
		t.Fatal("later rendering overwrote retained image")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.Chart.RequestedRenderMode() != chart.WebGPU {
		t.Fatal("renderer toggle failed")
	}
	m.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	if m.Chart.Camera() != initialCamera(s) {
		t.Fatal("reset did not restore camera")
	}
	if len(m.Scene.Buildings[0].Mesh.Vertices) != revision {
		t.Fatal("navigation changed source mesh")
	}
}
