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
	pending     image.Image
	pendingCrop image.Rectangle
	submitted   image.Image
	canvas      *panCanvas
	closed      bool
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
	return m.presentCrop(img, image.Rectangle{})
}

func (m *Model) presentCrop(img image.Image, crop image.Rectangle) tea.Cmd {
	if m.panState.closed {
		return nil
	}
	if img != nil && m.pic.Status().Busy {
		m.panState.pending = img
		m.panState.pendingCrop = crop
		return nil
	}
	m.panState.pending = nil
	if img != nil && !crop.Empty() && img == m.panState.submitted && m.pic.Status().Err == nil {
		return m.pic.SetCrop(crop)
	}
	m.panState.submitted = img
	return m.pic.SetImageImmutableCropped(img, crop)
}

func (m *Model) presentPending() tea.Cmd {
	if m.panState.pending == nil || m.pic.Status().Busy {
		return nil
	}
	return m.presentCrop(m.panState.pending, m.panState.pendingCrop)
}
