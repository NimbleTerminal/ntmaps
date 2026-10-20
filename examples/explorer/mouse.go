package main

import (
	"image"
	"math"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

type dragOwner uint8

const (
	dragNone dragOwner = iota
	dragMap
	dragCity
)

type pointerState struct {
	owner dragOwner
	last  image.Point
}

func (m *model) sidebarVisible() bool { return m.width >= 75 && m.height >= 16 }
func (m *model) mapRect() image.Rectangle {
	L := m.layout()
	x := 1
	if m.sidebarVisible() {
		x += L.leftInnerW + 2
	}
	return image.Rect(x, 1, x+L.mapInnerW, 1+L.mapInnerH)
}
func (m *model) cityPlotRect() image.Rectangle {
	r := m.mapRect()
	r.Min.Y++    // chart title
	r.Max.Y -= 5 // chart footer and the city pane's four status rows
	return r
}
func (m *model) listRect() image.Rectangle {
	L := m.layout()
	if !m.sidebarVisible() {
		return image.Rectangle{}
	}
	return image.Rect(1, 1, 1+L.leftInnerW, 1+L.listInnerH)
}

// The chart scans its own zones in pane coordinates. Translate the complete
// mouse event, including release/motion outside the pane, without losing type,
// button or modifiers. The app's sidebar and border are not part of that frame.
func localMouse(msg tea.MouseMsg, origin image.Point) tea.MouseMsg {
	v := msg.Mouse()
	v.X -= origin.X
	v.Y -= origin.Y
	switch msg.(type) {
	case tea.MouseClickMsg:
		return tea.MouseClickMsg(v)
	case tea.MouseReleaseMsg:
		return tea.MouseReleaseMsg(v)
	case tea.MouseWheelMsg:
		return tea.MouseWheelMsg(v)
	case tea.MouseMotionMsg:
		return tea.MouseMotionMsg(v)
	}
	return msg
}
func (m *model) cancelDrag() tea.Cmd {
	owner := m.pointer.owner
	m.pointer = pointerState{}
	if owner == dragCity && m.cityPane != nil {
		_, cmd := m.cityPane.Update(tea.MouseReleaseMsg{X: -1, Y: -1, Button: tea.MouseLeft})
		return cmd
	}
	return nil
}
func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	v := msg.Mouse()
	if _, wheel := msg.(tea.MouseWheelMsg); wheel && v.Button != tea.MouseWheelUp && v.Button != tea.MouseWheelDown {
		return nil
	}
	p := image.Pt(v.X, v.Y)
	if _, click := msg.(tea.MouseClickMsg); click && m.pointer.owner != dragNone {
		m.cancelDrag()
	}
	if _, release := msg.(tea.MouseReleaseMsg); release {
		owner := m.pointer.owner
		m.pointer = pointerState{}
		if owner == dragCity && m.cityPane != nil {
			_, cmd := m.cityPane.Update(localMouse(msg, m.mapRect().Min))
			return cmd
		}
		return nil
	}
	if _, motion := msg.(tea.MouseMotionMsg); motion && m.pointer.owner != dragNone && v.Button != tea.MouseLeft {
		return m.cancelDrag()
	}
	if m.pointer.owner == dragMap {
		if _, motion := msg.(tea.MouseMotionMsg); motion {
			dx, dy := p.X-m.pointer.last.X, p.Y-m.pointer.last.Y
			m.pointer.last = p
			return m.dragMap(dx, dy)
		}
	}
	if m.pointer.owner == dragCity && m.cityPane != nil {
		if _, motion := msg.(tea.MouseMotionMsg); motion {
			_, cmd := m.cityPane.Update(localMouse(msg, m.mapRect().Min))
			return cmd
		}
	}
	if p.In(m.listRect()) {
		switch msg.(type) {
		case tea.MouseWheelMsg:
			switch v.Button {
			case tea.MouseWheelUp:
				m.list.CursorUp()
			case tea.MouseWheelDown:
				m.list.CursorDown()
			default:
				return nil
			}
			return m.syncMapToSelection()
		case tea.MouseClickMsg:
			if v.Button != tea.MouseLeft {
				return nil
			}
			start := m.list.Paginator.Page * m.list.Paginator.PerPage
			end := min(start+m.list.Paginator.PerPage, len(m.list.VisibleItems()))
			for i := start; i < end; i++ {
				if m.zones.Get(m.itemZone(i)).InBounds(msg) {
					m.list.Select(i)
					return m.syncMapToSelection()
				}
			}
		}
		return nil
	}
	if m.list.FilterState() == list.Filtering {
		return nil
	}
	if m.in3D {
		if m.cityPane == nil || m.loading || m.loadErr != nil {
			return nil
		}
		if !p.In(m.cityPlotRect()) {
			// A motion outside the chart clears hover, but can't start a drag/zoom.
			if _, motion := msg.(tea.MouseMotionMsg); motion {
				_, cmd := m.cityPane.Update(localMouse(msg, m.mapRect().Min))
				return cmd
			}
			return nil
		}
		if _, click := msg.(tea.MouseClickMsg); click && v.Button == tea.MouseLeft {
			m.pointer = pointerState{owner: dragCity, last: p}
		}
		_, cmd := m.cityPane.Update(localMouse(msg, m.mapRect().Min))
		return cmd
	}
	r := m.mapRect()
	r.Max.Y-- // leave the map attribution strip outside the gesture area
	if !p.In(r) {
		return nil
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if v.Button == tea.MouseLeft {
			m.pointer = pointerState{owner: dragMap, last: p}
		}
	case tea.MouseWheelMsg:
		switch v.Button {
		case tea.MouseWheelUp:
			return m.zoomMap(1)
		case tea.MouseWheelDown:
			return m.zoomMap(-1)
		}
	}
	return nil
}

// the widget's geographic canvas uses 8x16 map pixels per terminal cell, before
// oversampling (whose extra zoom cancels the extra density). Preserve that
// scale and optical crop when translating a grab-and-drag into a new center.
func (m *model) dragMap(dx, dy int) tea.Cmd {
	if dx == 0 && dy == 0 {
		return nil
	}
	lat, lon := m.mv.Center()
	lat = max(-85.0511287798066, min(85.0511287798066, lat))
	world := 256 * math.Exp2(float64(m.mv.Zoom()))
	crop := math.Exp2(float64(m.mv.OpticalZoom()))
	x := (lon+180)/360*world - float64(dx)*8/crop
	y := (1-math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi)/2*world - float64(dy)*16/crop
	x = math.Mod(math.Mod(x, world)+world, world)
	y = max(0, min(world, y))
	lon = x/world*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*y/world))) * 180 / math.Pi
	m.mv.SetLatLng(lat, lon, m.mv.Zoom())
	return m.mv.Init()
}

func (m *model) zoomMap(delta int) tea.Cmd {
	zoom := max(2, min(19, m.mv.Zoom()+delta))
	if zoom == m.mv.Zoom() {
		return nil
	}
	lat, lon := m.mv.Center()
	m.mv.SetLatLng(lat, lon, zoom)
	return m.mv.Init()
}
