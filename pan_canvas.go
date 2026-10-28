package ntmaps

import (
	tea "charm.land/bubbletea/v2"
	"image"
	"image/draw"
	"math"
	"nimbleterminal.dev/ntmaps/tile"
)

// panCanvas retains immutable pixels around the viewport. Its key excludes
// location; all other render inputs must still match before reusing pixels.
type panCanvas struct {
	image    image.Image
	key      renderKey
	geom     viewGeometry
	viewport image.Rectangle
}

// cachedPan never fetches extra tiles. Once the surrounding tiles are cached,
// compose one larger source and pan it using Picture's source rectangle API.
func (m *Model) cachedPan(geom viewGeometry, key renderKey) (tea.Cmd, bool) {
	if m.pic.Mode() != RenderKitty || geom.w != geom.cellRectW || geom.h != geom.cellRectH ||
		(geom.attribution() != "" && m.picRows() == m.rows) {
		m.panState.canvas = nil
		return nil, false
	}
	key.lat, key.lng = 0, 0
	canvas := m.panState.canvas
	cropFor := func(c *panCanvas) image.Rectangle {
		_, _, world := tile.Project(0, 0, geom.zoom, m.info().Size())
		dx := geom.cx - c.geom.cx
		dx -= math.Floor(dx/world+0.5) * world
		x := int(math.Floor(c.geom.cx+dx)) - int(math.Floor(c.geom.cx)) + (c.geom.w-geom.w)/2
		y := int(math.Floor(geom.cy)) - int(math.Floor(c.geom.cy)) + (c.geom.h-geom.h)/2
		return image.Rect(x, y, x+geom.w, y+geom.h)
	}
	if canvas == nil || canvas.key != key || !cropFor(canvas).In(canvas.image.Bounds()) {
		m.panState.canvas = nil
		expanded := geom
		// Half a viewport of slack on each side, bounded by ntpicture's upload cap.
		expanded.w, expanded.h = 2*geom.w, 2*geom.h
		const maxPanPixels = 16 << 20 // ntpicture's whole-source panning limit
		if expanded.w <= 0 || expanded.h <= 0 || expanded.w > maxPanPixels/expanded.h {
			return nil, false
		}

		// Avoid ambiguous repeated-world marker/path copies at very low zoom.
		_, _, world := tile.Project(0, 0, geom.zoom, m.info().Size())
		if float64(expanded.w) >= world {
			return nil, false
		}
		expanded.cellRectW, expanded.cellRectH = expanded.w, expanded.h
		expanded.noAttribution = true // the fixed terminal attribution row stays visible
		placements := expanded.placements()
		if !m.tilesCached(expanded, placements) {
			return nil, false
		}
		img, _ := expanded.compose(placements, func(src tile.Source, id tile.ID) (image.Image, bool) {
			if m.loader == nil {
				return nil, false
			}
			return m.loader.Cached(src, id)
		})
		canvas = &panCanvas{image: img, key: key, geom: expanded}
		m.panState.canvas = canvas
	}
	crop := cropFor(canvas)
	canvas.viewport = crop
	// Keep Image()'s viewport-sized, zero-origin bitmap and embedded attribution
	// contract for screenshots/3D consumers. This is a row copy, not a tile render.
	snapshot := image.NewRGBA(image.Rect(0, 0, geom.w, geom.h))
	draw.Draw(snapshot, snapshot.Bounds(), canvas.image, crop.Min, draw.Src)
	drawMapAttribution(snapshot, geom.attribution())
	m.sourceImage = snapshot
	*m.lastAccepted = *m.renderGen
	m.errMsg = ""
	return tea.Batch(m.presentCrop(canvas.image, opticalCropRect(crop, opticalCropFactor(m.opticalZoom))), m.prefetchCmd(geom)), true
}

func opticalCropRect(b image.Rectangle, factor int) image.Rectangle {
	if factor <= 1 {
		return b
	}
	w, h := b.Dx(), b.Dy()
	cw, ch := w/factor, h/factor
	cw -= (w - cw) & 1
	ch -= (h - ch) & 1
	if cw < 1 || ch < 1 {
		return b
	}
	return image.Rect(b.Min.X+(w-cw)/2, b.Min.Y+(h-ch)/2, b.Min.X+(w+cw)/2, b.Min.Y+(h+ch)/2)
}
