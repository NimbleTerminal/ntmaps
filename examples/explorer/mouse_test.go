package main

import (
	"fmt"
	"image"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func clickAt(p image.Point) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft}
}
func moveAt(p image.Point) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft}
}
func releaseAt(p image.Point) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: p.X, Y: p.Y, Button: tea.MouseLeft}
}
func wheelAt(p image.Point, button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: p.X, Y: p.Y, Button: button}
}
func middle(r image.Rectangle) image.Point { return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2) }

func TestExplorerEnablesMouseReporting(t *testing.T) {
	m := fixture(t)
	if m.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("outer view dropped mouse reporting")
	}
	enterCity(t, m)
	if m.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("3D view does not request drag events")
	}
}
func TestCityMouseCoordinatesAndCapture(t *testing.T) {
	for _, size := range [][2]int{{100, 40}, {60, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := fixture(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			enterCity(t, m)
			m.View()
			p := middle(m.cityPlotRect())
			before := m.cityPane.Chart.Camera()
			m.Update(wheelAt(p, tea.MouseWheelUp))
			zoomed := m.cityPane.Chart.Camera()
			if zoomed.Distance >= before.Distance {
				t.Fatal("wheel at displayed city did not zoom")
			}
			m.Update(clickAt(p))
			m.Update(moveAt(p.Add(image.Pt(4, 2))))
			turned := m.cityPane.Chart.Camera()
			if turned.Beta != zoomed.Beta+8 || turned.Alpha != zoomed.Alpha+4 {
				t.Fatalf("drag ignored pane offset: %+v -> %+v", zoomed, turned)
			}
			outside := image.Pt(-3, -3)
			m.Update(moveAt(outside))
			moved := m.cityPane.Chart.Camera()
			if moved.Beta == turned.Beta {
				t.Fatal("captured drag stopped at pane boundary")
			}
			m.Update(releaseAt(outside))
			m.Update(moveAt(p))
			if m.cityPane.Chart.Camera() != moved {
				t.Fatal("release outside left city dragging")
			}
			m.Update(wheelAt(image.Pt(0, 0), tea.MouseWheelDown))
			if m.cityPane.Chart.Camera() != moved {
				t.Fatal("border scroll zoomed city")
			}
		})
	}
}
func TestCityShiftDragAndModeSwitchRelease(t *testing.T) {
	m := fixture(t)
	enterCity(t, m)
	m.View()
	p := middle(m.cityPlotRect())
	before := m.cityPane.Chart.Camera()
	m.Update(clickAt(p))
	shift := moveAt(p.Add(image.Pt(3, 2)))
	shift.Mod = tea.ModShift
	m.Update(shift)
	after := m.cityPane.Chart.Camera()
	if after.Target == before.Target || after.Beta != before.Beta || after.Alpha != before.Alpha {
		t.Fatal("shift drag did not pan")
	}
	m.Update(press("v"))
	if m.pointer.owner != dragNone {
		t.Fatal("mode switch retained drag capture")
	}
	m.Update(press("v"))
	m.View()
	saved := m.cityPane.Chart.Camera()
	m.Update(moveAt(p))
	if m.cityPane.Chart.Camera() != saved {
		t.Fatal("old drag resumed after mode switch")
	}
}
func TestMapMouseDragZoomAndRelease(t *testing.T) {
	m := fixture(t)
	p := middle(m.mapRect())
	lat, lon := m.mv.Center()
	zoom := m.mv.Zoom()
	m.Update(clickAt(p))
	m.Update(moveAt(p.Add(image.Pt(4, 2))))
	a, b := m.mv.Center()
	if a <= lat || b >= lon {
		t.Fatal("grab drag did not move map with pointer")
	}
	m.Update(releaseAt(image.Pt(0, 0)))
	m.Update(moveAt(p))
	c, d := m.mv.Center()
	if c != a || d != b {
		t.Fatal("outside release left map dragging")
	}
	m.Update(wheelAt(p, tea.MouseWheelUp))
	if m.mv.Zoom() != zoom+1 {
		t.Fatal("wheel up did not zoom at initial high map zoom")
	}
	m.Update(wheelAt(p, tea.MouseWheelDown))
	if m.mv.Zoom() != zoom {
		t.Fatal("wheel down did not restore zoom")
	}
	m.Update(wheelAt(image.Pt(0, 0), tea.MouseWheelDown))
	if m.mv.Zoom() != zoom {
		t.Fatal("border scroll affected map")
	}
}
func TestSidebarMouseSelectsWithoutPanningMap(t *testing.T) {
	m := fixture(t)
	m.View()
	before := m.list.Index()
	p := middle(m.listRect())
	m.Update(wheelAt(p, tea.MouseWheelDown))
	if m.list.Index() == before || m.lastPlace != m.places[m.list.Index()] {
		t.Fatal("sidebar wheel did not choose a place")
	}
	m.View()
	z := m.zones.Get(m.itemZone(0))
	if z.IsZero() {
		t.Fatal("list item has no mouse zone")
	}
	m.Update(clickAt(image.Pt(z.StartX, z.StartY)))
	if m.list.Index() != 0 || m.lastPlace != m.places[0] {
		t.Fatal("list click did not select the displayed item")
	}
	if m.pointer.owner != dragNone {
		t.Fatal("sidebar click captured map drag")
	}
}
