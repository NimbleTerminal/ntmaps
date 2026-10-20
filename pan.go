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

// panState holds the spring motion and the Kitty presentation
// back-pressure. Only the Bubble Tea event loop changes it; commands use
// its pointer to identify the model their messages belong to.
type panState struct {
	motion panMotion

	// encoding is set while a Kitty frame is being encoded and sent.
	// Images presented meanwhile wait in pending; the latest wins.
	encoding bool
	pending  image.Image
}

type panPictureMsg struct {
	owner *panState
	msg   tea.Msg
}
type panPresentedMsg struct{ owner *panState }

func (m *Model) currentRenderKey() renderKey {
	os, _ := m.effectiveOversample()
	key := makeRenderKey(m.lat, m.lng, m.zoom, m.cols, m.picRows(), m.info().Name,
		os, m.maxAspectRatio, m.letterboxColor, m.markers)
	key.paths = pathsKey(m.paths)
	return key
}

// presentImage hands img to the picture model. While a Kitty frame is
// still being encoded the image is queued instead, so rapid input cannot
// invalidate every frame before one is sent; the queued image is
// presented when panPresentedMsg arrives.
func (m *Model) presentImage(img image.Image) tea.Cmd {
	state := m.panState
	if state.encoding {
		state.pending = img
		return nil
	}
	cmd := m.pic.SetImage(img)
	if cmd == nil {
		return nil
	}
	state.encoding = true
	state.pending = nil
	return func() tea.Msg { return panPictureMsg{owner: state, msg: cmd()} }
}
