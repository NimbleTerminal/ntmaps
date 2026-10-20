package ntmaps

import (
	"image"
	"math"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/tile"
)

// Mouse support. The widget handles tea.MouseMsg in its own cell
// coordinates, with (0, 0) the top-left cell of the map: a left-button
// drag pans, and the wheel zooms around the cursor. Events outside the
// map cells are ignored except while a drag is in progress, so a host
// that places the map away from the terminal origin should translate
// events with OffsetMouse before forwarding them, and enable mouse
// reporting in its View (tea.MouseModeCellMotion or AllMotion).

// dragState tracks a left-button drag across the map.
type dragState struct {
	active bool
	last   image.Point
}

// Dragging reports whether a mouse drag is in progress. Hosts with
// several panes use it to keep routing motion and release events to the
// map after the pointer leaves it.
func (m Model) Dragging() bool { return m.drag.active }

// OffsetMouse returns msg with its coordinates shifted so that (x, y) in
// the terminal becomes (0, 0), preserving the event's type, button, and
// modifiers. Hosts call it with the map's top-left cell before
// forwarding a mouse event to Update.
func OffsetMouse(msg tea.MouseMsg, x, y int) tea.MouseMsg {
	v := msg.Mouse()
	v.X -= x
	v.Y -= y
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

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	v := msg.Mouse()
	p := image.Pt(v.X, v.Y)
	inside := p.In(image.Rect(0, 0, m.cols, m.picRows()))
	switch msg.(type) {
	case tea.MouseClickMsg:
		if v.Button == tea.MouseLeft && inside {
			m.stopAnimation()
			m.drag = dragState{active: true, last: p}
		} else {
			m.drag = dragState{}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		m.drag = dragState{}
		return m, nil
	case tea.MouseMotionMsg:
		if !m.drag.active {
			return m, nil
		}
		if v.Button != tea.MouseLeft {
			// The button went up somewhere we did not see.
			m.drag = dragState{}
			return m, nil
		}
		dx, dy := p.X-m.drag.last.X, p.Y-m.drag.last.Y
		m.drag.last = p
		if dx == 0 && dy == 0 {
			return m, nil
		}
		m.stopAnimation()
		// The map moves with the pointer, so the center moves the other
		// way; panPixels already accounts for optical zoom.
		m.panPixels(float64(-dx*osmPxPerCellW), float64(-dy*osmPxPerCellH))
		return m, m.renderMapCmd()
	case tea.MouseWheelMsg:
		if !inside {
			return m, nil
		}
		switch v.Button {
		case tea.MouseWheelUp:
			return m.zoomAt(1, p)
		case tea.MouseWheelDown:
			return m.zoomAt(-1, p)
		}
	}
	return m, nil
}

// zoomAt changes the zoom by delta while keeping the geographic point
// under cell p where it is, so wheel zooming homes in on the cursor.
func (m Model) zoomAt(delta int, p image.Point) (Model, tea.Cmd) {
	zoom := m.clampZoom(m.zoom + delta)
	if zoom == m.zoom {
		return m, nil
	}
	size := m.info().Size()
	crop := float64(opticalCropFactor(m.opticalZoom))
	offX := (float64(p.X) + 0.5 - float64(m.cols)/2) * osmPxPerCellW / crop
	offY := (float64(p.Y) + 0.5 - float64(m.picRows())/2) * osmPxPerCellH / crop
	cx, cy, _ := tile.Project(m.lat, m.lng, m.zoom, size)
	scale := math.Exp2(float64(zoom - m.zoom))
	m.lat, m.lng = tile.Unproject((cx+offX)*scale-offX, (cy+offY)*scale-offY, zoom, size)
	m.zoom = zoom
	m.stopAnimation()
	return m, m.renderMapCmd()
}

// clampZoom keeps z within the arrow-key range for the current source.
func (m Model) clampZoom(z int) int {
	return max(minZoom, min(m.info().Max(), z))
}

// stopAnimation cancels a spring pan so a gesture takes over cleanly.
func (m *Model) stopAnimation() {
	if m.panState != nil {
		m.panState.motion.active = false
	}
}
