package main

import (
	"image"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps"
)

type dragOwner uint8

const (
	dragNone dragOwner = iota
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
			_, cmd := m.cityPane.Update(ntmaps.OffsetMouse(msg, m.mapRect().Min.X, m.mapRect().Min.Y))
			return cmd
		}
		if m.mv.Dragging() {
			return m.mapMouse(msg)
		}
		return nil
	}
	if _, motion := msg.(tea.MouseMotionMsg); motion && m.pointer.owner != dragNone && v.Button != tea.MouseLeft {
		return m.cancelDrag()
	}
	if m.mv.Dragging() {
		// The map keeps the drag even when the pointer leaves it.
		return m.mapMouse(msg)
	}
	if m.pointer.owner == dragCity && m.cityPane != nil {
		if _, motion := msg.(tea.MouseMotionMsg); motion {
			_, cmd := m.cityPane.Update(ntmaps.OffsetMouse(msg, m.mapRect().Min.X, m.mapRect().Min.Y))
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
				_, cmd := m.cityPane.Update(ntmaps.OffsetMouse(msg, m.mapRect().Min.X, m.mapRect().Min.Y))
				return cmd
			}
			return nil
		}
		if _, click := msg.(tea.MouseClickMsg); click && v.Button == tea.MouseLeft {
			m.pointer = pointerState{owner: dragCity, last: p}
		}
		_, cmd := m.cityPane.Update(ntmaps.OffsetMouse(msg, m.mapRect().Min.X, m.mapRect().Min.Y))
		return cmd
	}
	r := m.mapRect()
	r.Max.Y-- // leave the map attribution strip outside the gesture area
	if !p.In(r) {
		return nil
	}
	return m.mapMouse(msg)
}

// mapMouse forwards a mouse event to the map widget in its own cell
// coordinates; the widget pans on drag and zooms on the wheel.
func (m *model) mapMouse(msg tea.MouseMsg) tea.Cmd {
	var cmd tea.Cmd
	m.mv, cmd = m.mv.Update(ntmaps.OffsetMouse(msg, m.mapRect().Min.X, m.mapRect().Min.Y))
	return cmd
}
