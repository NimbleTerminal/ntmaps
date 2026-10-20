package ntmaps

import (
	"image"
	"math"
	"testing"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/tile"
)

func mouseTestModel() Model {
	m := NewWithConfig(Config{Cols: 20, Rows: 11, DisableDiskCache: true, Prefetch: -1})
	m.SetLatLng(10, 20, 12)
	m.source = tile.Provider{}
	return m
}

func click(x, y int) tea.MouseClickMsg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }
func drag(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft} }
func release(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func wheel(x, y int, b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: y, Button: b}
}

func TestMouseDragPansByCells(t *testing.T) {
	m := mouseTestModel()
	x, y, _ := worldPixels(m.lat, m.lng, m.zoom, 256)
	m, _ = m.Update(click(5, 5))
	if !m.Dragging() {
		t.Fatal("left click inside the map should start a drag")
	}
	m, _ = m.Update(drag(7, 6))
	nx, ny, _ := worldPixels(m.lat, m.lng, m.zoom, 256)
	if math.Abs(nx-x+16) > 1e-6 || math.Abs(ny-y+16) > 1e-6 {
		t.Fatalf("drag moved the center by (%g,%g) px, want (-16,-16)", nx-x, ny-y)
	}
	m, _ = m.Update(release(-3, -3))
	if m.Dragging() {
		t.Fatal("release should end the drag even outside the map")
	}
	m, _ = m.Update(drag(9, 9))
	if ax, ay, _ := worldPixels(m.lat, m.lng, m.zoom, 256); ax != nx || ay != ny {
		t.Fatal("motion after release must not pan")
	}
}

func TestMouseDragHonorsOpticalZoomAndBounds(t *testing.T) {
	m := mouseTestModel()
	m.SetLatLng(0, 0, 16)
	_, lngNormal := dragBy(m, 4)
	m.SetLatLng(0, 0, 16)
	m.SetOpticalZoom(1)
	_, lngCropped := dragBy(m, 4)
	if math.Abs(lngCropped-lngNormal/2) > 1e-8 {
		t.Fatalf("optical zoom should halve the drag: %g vs %g", lngCropped, lngNormal)
	}
	m.SetLatLng(85, 179.999, 16)
	m, _ = m.Update(click(1, 1))
	m, _ = m.Update(drag(-100000, 100000))
	lat, lng := m.Center()
	if math.IsNaN(lat) || math.Abs(lat) > tile.MaxLatitude+1e-9 || lng < -180 || lng > 180 {
		t.Fatalf("drag left Mercator bounds %g,%g", lat, lng)
	}
}

func dragBy(m Model, cells int) (lat, lng float64) {
	m, _ = m.Update(click(2, 2))
	m, _ = m.Update(drag(2+cells, 2))
	return m.Center()
}

func TestMouseIgnoresClicksOutsideAndOtherButtons(t *testing.T) {
	m := mouseTestModel()
	for _, c := range []tea.MouseClickMsg{
		click(25, 2), // right of the map
		click(2, 10), // attribution row
		click(2, -1), // above
		{X: 2, Y: 2, Button: tea.MouseRight},
	} {
		m, _ = m.Update(c)
		if m.Dragging() {
			t.Fatalf("click %v started a drag", c)
		}
	}
	m, _ = m.Update(click(3, 3))
	m, _ = m.Update(tea.MouseMotionMsg{X: 5, Y: 5, Button: tea.MouseNone})
	if m.Dragging() {
		t.Fatal("motion without the button should end the drag")
	}
}

func TestMouseWheelZoomsAroundCursor(t *testing.T) {
	m := mouseTestModel()
	p := image.Pt(3, 2)
	under := func(m Model) (float64, float64) {
		cx, cy, _ := worldPixels(m.lat, m.lng, m.zoom, 256)
		offX := (float64(p.X) + 0.5 - float64(m.cols)/2) * osmPxPerCellW
		offY := (float64(p.Y) + 0.5 - float64(m.picRows())/2) * osmPxPerCellH
		return tile.Unproject(cx+offX, cy+offY, m.zoom, 256)
	}
	lat0, lng0 := under(m)
	m, _ = m.Update(wheel(p.X, p.Y, tea.MouseWheelUp))
	if m.Zoom() != 13 {
		t.Fatalf("zoom = %d, want 13", m.Zoom())
	}
	lat1, lng1 := under(m)
	if math.Abs(lat1-lat0) > 1e-9 || math.Abs(lng1-lng0) > 1e-9 {
		t.Fatalf("point under cursor moved from (%g,%g) to (%g,%g)", lat0, lng0, lat1, lng1)
	}
	m, _ = m.Update(wheel(p.X, p.Y, tea.MouseWheelDown))
	lat2, lng2 := under(m)
	if m.Zoom() != 12 || math.Abs(lat2-lat0) > 1e-9 || math.Abs(lng2-lng0) > 1e-9 {
		t.Fatalf("zoom back = %d at (%g,%g)", m.Zoom(), lat2, lng2)
	}
	m, _ = m.Update(wheel(25, 2, tea.MouseWheelUp))
	if m.Zoom() != 12 {
		t.Fatal("wheel outside the map changed the zoom")
	}
	m.zoom = m.info().Max()
	m, _ = m.Update(wheel(p.X, p.Y, tea.MouseWheelUp))
	if m.Zoom() != m.info().Max() {
		t.Fatal("wheel zoomed past the source's maximum")
	}
	m, _ = m.Update(wheel(p.X, p.Y, tea.MouseWheelLeft))
	if m.Zoom() != m.info().Max() {
		t.Fatal("horizontal wheel should not zoom")
	}
}

func TestMouseDragStopsSpring(t *testing.T) {
	m := animatedTestModel()
	m, _ = m.arrowPan(16, 0)
	if !m.animating() {
		t.Fatal("arrow should start the spring")
	}
	m, _ = m.Update(click(4, 4))
	if m.animating() {
		t.Fatal("a click should cancel the spring so the drag takes over")
	}
}

func TestOffsetMousePreservesTypeAndButton(t *testing.T) {
	got := OffsetMouse(tea.MouseWheelMsg{X: 10, Y: 7, Button: tea.MouseWheelDown, Mod: tea.ModShift}, 4, 2)
	w, ok := got.(tea.MouseWheelMsg)
	if !ok || w.X != 6 || w.Y != 5 || w.Button != tea.MouseWheelDown || w.Mod != tea.ModShift {
		t.Fatalf("OffsetMouse = %#v", got)
	}
	if r, ok := OffsetMouse(release(3, 3), 1, 1).(tea.MouseReleaseMsg); !ok || r.X != 2 {
		t.Fatalf("release offset = %#v", r)
	}
	if IsMapOwnMsg(click(0, 0)) {
		t.Fatal("mouse events are terminal input, not the widget's own messages")
	}
}
