package cityview

import (
	"bytes"
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"nimbleterminal.dev/ntmaps/internal/city"
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

// Exercise the actual presentation commands; Snapshot alone cannot detect a
// camera update that never reaches the terminal.
func TestKittyOrbitPresentsWithoutGlyphToggle(t *testing.T) {
	for _, capability := range []picture.KittyCapability{picture.KittyCapabilityUnknown, picture.KittyCapabilityUnsupported, picture.KittyCapabilitySupported} {
		t.Run(fmt.Sprint(capability), func(t *testing.T) {
			d, s := fixture(t)
			session := picture.NewTerminalSession(picture.TerminalOptions{
				FrameEdits:  capability,
				FrameAppend: picture.KittyCapabilityUnsupported, FrameSelect: picture.KittyCapabilityUnsupported,
				Kitty:            picture.KittyCapabilitySupported,
				SharedMemory:     picture.KittyCapabilityUnsupported,
				VirtualPlacement: picture.KittyCapabilityUnsupported,
				CellPixelWidth:   1, CellPixelHeight: 2,
			})
			defer session.Close()
			m, err := New(d, s, chart.Software, picture.KittyMediumDirect, session)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			drainCity(t, m, m.SetSize(36, 20))
			initial := drainCity(t, m, m.Init())
			if !strings.Contains(initial, "a=T") {
				t.Fatal("initial Kitty image was not uploaded")
			}
			// Bubblezone processes scanned bounds asynchronously.
			m.View()
			time.Sleep(10 * time.Millisecond)
			before := m.Chart.Camera()
			_, cmd := m.Update(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft})
			drainCity(t, m, cmd)
			_, cmd = m.Update(tea.MouseMotionMsg{X: 14, Y: 7, Button: tea.MouseLeft})
			out := drainCity(t, m, cmd)
			if m.Chart.Camera() == before {
				t.Fatal("first drag did not orbit")
			}
			status := m.Chart.PictureStatus()
			if status.Busy || status.Presented != status.Generation {
				t.Fatalf("orbit did not finish presenting: %+v", status)
			}
			edits := capability == picture.KittyCapabilitySupported
			if strings.Contains(out, "a=f") != edits || strings.Contains(out, "a=T") == edits {
				t.Fatalf("orbit used the wrong capability path: %q", out)
			}
			t.Logf("initial=%d bytes orbit=%d bytes frame-edit=%v", len(initial), len(out), strings.Contains(out, "a=f"))
		})
	}
}

func drainCity(t *testing.T, m *Model, cmd tea.Cmd) string {
	t.Helper()
	var out strings.Builder
	steps := 0
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		steps++
		if steps > 1000 {
			t.Fatal("presentation did not settle")
		}
		msg := cmd()
		if msg == nil {
			return
		}
		v := reflect.ValueOf(msg)
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				run(v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		if raw, ok := msg.(tea.RawMsg); ok {
			fmt.Fprint(&out, raw.Msg)
			return
		}
		_, next := m.Update(msg)
		run(next)
	}
	run(cmd)
	return out.String()
}
