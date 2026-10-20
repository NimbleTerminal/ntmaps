package ntmaps

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/tile"
)

const panRenderInterval = time.Second / 30

// worldPixels converts latitude and longitude to Web Mercator pixels.
func worldPixels(lat, lng float64, zoom, tileSize int) (x, y, world float64) {
	return tile.Project(lat, lng, zoom, tileSize)
}

func (m *Model) panPixels(dx, dy float64) {
	tileSize := m.provider.Size()
	x, y, _ := worldPixels(m.lat, m.lng, m.zoom, tileSize)
	// Compensate for optical zoom to keep the on-screen pan distance constant.
	factor := float64(opticalCropFactor(m.opticalZoom))
	m.lat, m.lng = tile.Unproject(x+dx/factor, y+dy/factor, m.zoom, tileSize)
}

// Only the Bubble Tea event loop changes panState. Commands use its pointer
// to identify the model and return buffers without modifying this state.
type panState struct {
	motion                      panMotion
	buffer                      *panBuffer
	busy, waiting               bool
	lastStart                   time.Time
	target, displayed           renderKey
	targetSet, displayedSet     bool
	visible                     image.Image
	encoding, presentationDirty bool
}

type panRenderTickMsg struct{ owner *panState }
type panPictureMsg struct {
	owner *panState
	msg   tea.Msg
}
type panPresentedMsg struct{ owner *panState }
type bufferImageMsg struct {
	owner  *panState
	buffer *panBuffer
	err    error
}

type panBuffer struct {
	key                 renderKey
	img                 image.Image
	centerX, centerY    float64
	world               float64
	mapW, mapH, padding int
	tileSize            int
	attribution         string
}

func (b *panBuffer) matches(key renderKey) bool {
	if b == nil {
		return false
	}
	other := b.key
	other.lat, other.lng = key.lat, key.lng
	return other == key
}

func (b *panBuffer) viewport(key renderKey) (image.Rectangle, bool) {
	if !b.matches(key) || b.img == nil {
		return image.Rectangle{}, false
	}
	x, y, _ := worldPixels(key.lat, key.lng, key.zoom+log2(key.oversample), b.tileSize)
	dx := x - b.centerX
	dx -= math.Floor(dx/b.world+0.5) * b.world
	// Match go-staticmaps by rounding the center down to whole pixels.
	ox := b.padding + int(math.Floor(b.centerX+dx)-math.Floor(b.centerX))
	oy := b.padding + int(math.Floor(y)-math.Floor(b.centerY))
	r := image.Rect(ox, oy, ox+b.mapW, oy+b.mapH)
	return r, r.In(b.img.Bounds())
}

func (b *panBuffer) nearEdge(r image.Rectangle) bool {
	margin := b.padding / 2
	bounds := b.img.Bounds()
	return r.Min.X < bounds.Min.X+margin || r.Min.Y < bounds.Min.Y+margin ||
		r.Max.X > bounds.Max.X-margin || r.Max.Y > bounds.Max.Y-margin
}

func (b *panBuffer) crop(r image.Rectangle, key renderKey, bg color.Color) image.Image {
	// Copy the viewport so cached views can release the full buffer
	// and attribution can be drawn without changing it.
	img := image.NewRGBA(image.Rect(0, 0, b.mapW, b.mapH))
	draw.Draw(img, img.Bounds(), b.img, r.Min, draw.Src)
	drawMapAttribution(img, b.attribution)
	return composeLetterbox(img, key.cols*osmPxPerCellW*key.oversample,
		key.rows*osmPxPerCellH*key.oversample, bg)
}

func (m *Model) currentRenderKey() renderKey {
	os, _ := m.effectiveOversample()
	return makeRenderKey(m.lat, m.lng, m.zoom, m.cols, m.picRows(), m.provider.Name,
		os, m.maxAspectRatio, m.letterboxColor, m.markers)
}

func (m *Model) renderBufferedMapCmd() tea.Cmd {
	state := m.panState
	key := m.currentRenderKey()
	if !state.targetSet || state.target != key {
		state.target, state.targetSet = key, true
		*m.renderGen++
	}

	var present tea.Cmd
	r, covered := state.buffer.viewport(key)
	if !state.displayedSet || state.displayed != key {
		var img image.Image
		if covered {
			img = state.buffer.crop(r, key, m.letterboxColor)
		} else if m.cache != nil {
			img, _ = m.cache.get(key)
		}
		if img != nil {
			state.visible = img
			state.displayed, state.displayedSet = key, true
			if m.cache != nil && !state.motion.active {
				m.cache.put(key, img)
			}
			present = m.presentPanImage(opticalCrop(img, opticalCropFactor(m.opticalZoom)))
		}
	}
	if state.displayedSet && state.displayed == key {
		m.sourceImage = state.visible
		m.errMsg = ""
		*m.lastAccepted = *m.renderGen
	}
	if covered && !state.buffer.nearEdge(r) {
		return present
	}
	if state.busy || state.waiting {
		return present
	}

	// Limit fetch frequency. New input does not delay a scheduled fetch.
	if delay := time.Until(state.lastStart.Add(panRenderInterval)); delay > 0 {
		state.waiting = true
		return tea.Batch(present, tea.Tick(delay, func(time.Time) tea.Msg {
			return panRenderTickMsg{owner: state}
		}))
	}
	state.busy = true
	state.lastStart = time.Now()
	provider := m.provider
	loader := m.loader
	markers := append([]Marker(nil), m.markers...)
	w, h := targetMapDims(key.cols*osmPxPerCellW*key.oversample,
		key.rows*osmPxPerCellH*key.oversample, key.maxAspectRatio)
	tileSize := provider.Size()
	tileZoom := key.zoom + log2(key.oversample)
	padding := m.panBufferTiles * tileSize
	x, y, world := worldPixels(key.lat, key.lng, tileZoom, tileSize)
	return tea.Batch(present, func() tea.Msg {
		// Attribution is drawn after cropping to the visible viewport.
		img, err := renderBuffer(context.Background(), loader, provider, markers, x, y, tileZoom, w+2*padding, h+2*padding)
		return bufferImageMsg{owner: state, err: err, buffer: &panBuffer{
			key: key, img: img, centerX: x, centerY: y, world: world,
			mapW: w, mapH: h, padding: padding, tileSize: tileSize,
			attribution: provider.Attribution,
		}}
	})
}

// Finish sending each Kitty frame before encoding the latest view.
// Otherwise, rapid input can invalidate every frame before it is sent.
func (m *Model) presentPanImage(img image.Image) tea.Cmd {
	state := m.panState
	if state.encoding {
		state.presentationDirty = true
		return nil
	}
	cmd := m.pic.SetImage(img)
	if cmd == nil {
		return nil
	}
	state.encoding = true
	return func() tea.Msg { return panPictureMsg{owner: state, msg: cmd()} }
}
