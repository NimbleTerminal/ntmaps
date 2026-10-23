package ntmaps

import (
	"image"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/tile"
)

// worldPixels converts latitude and longitude to Web Mercator pixels.
func worldPixels(lat, lng float64, zoom, tileSize int) (x, y, world float64) {
	return tile.Project(lat, lng, zoom, tileSize)
}

func (m *Model) panPixels(dx, dy float64) {
	tileSize := m.info().Size()
	x, y, _ := worldPixels(m.lat, m.lng, m.zoom, tileSize)
	// Compensate for optical zoom to keep the on-screen pan distance constant.
	factor := float64(opticalCropFactor(m.opticalZoom))
	m.lat, m.lng = tile.Unproject(x+dx/factor, y+dy/factor, m.zoom, tileSize)
}

// panState holds the spring motion and presentation
// back-pressure. Only the Bubble Tea event loop changes it; commands use
// its pointer to identify the model their messages belong to.
type panState struct {
	motion panMotion

	// pending retains only the newest immutable render while Picture is busy.
	pending image.Image
	closed  bool
}

func (m *Model) currentRenderKey() renderKey {
	os, _ := m.effectiveOversample()
	key := makeRenderKey(m.lat, m.lng, m.zoom, m.cols, m.picRows(), m.info().Name,
		os, m.maxAspectRatio, m.letterboxColor, m.markers)
	key.paths = pathsKey(m.paths)
	key.overlays = overlayKey(m.overlays)
	return key
}

// presentImage lets an asynchronous presentation finish before submitting the
// newest pending image. This prevents rapid input from starving either renderer.
func (m *Model) presentImage(img image.Image) tea.Cmd {
	if m.panState.closed {
		return nil
	}
	if img != nil && m.pic.Status().Busy {
		m.panState.pending = img
		return nil
	}
	m.panState.pending = nil
	return m.pic.SetImageImmutable(img)
}

func (m *Model) presentPending() tea.Cmd {
	if m.panState.pending == nil || m.pic.Status().Busy {
		return nil
	}
	return m.presentImage(m.panState.pending)
}
