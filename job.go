package ntmaps

import (
	"context"
	"image"
	"image/color"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"nimbleterminal.dev/ntmaps/tile"
)

// progressInterval is the shortest gap between two partial repaints of
// one render. Tiles that land faster than this are batched into one
// mapImageMsg so the terminal is not re-encoded for every tile.
const progressInterval = 100 * time.Millisecond

// viewGeometry is everything a render needs to turn tiles into the map
// portion of the source image: the pixel size of the map, the world pixel
// at its center, and the tile zoom.
type viewGeometry struct {
	source  tile.Source
	zoom    int // tile zoom, after any oversample boost
	w, h    int // map portion in pixels (before letterbox)
	cx, cy  float64
	markers []Marker
	paths   []Path

	// cellRectW/H and letterbox pad the map portion to the full cell
	// rectangle; they equal w/h when MaxAspectRatio is off.
	cellRectW, cellRectH int
	letterbox            color.Color
}

func (g viewGeometry) placements() []tile.Placement {
	return tile.Cover(g.cx, g.cy, g.w, g.h, g.zoom, infoOf(g.source).Size())
}

// compose paints the map from whatever lookup can supply, draws markers
// and the attribution strip, and pads to the cell rectangle. It returns
// the in-world tiles that were missing.
func (g viewGeometry) compose(placements []tile.Placement, lookup func(tile.ID) (image.Image, bool)) (image.Image, []tile.ID) {
	info := infoOf(g.source)
	img := image.NewRGBA(image.Rect(0, 0, g.w, g.h))
	missing := composeTiles(img, placements, info, lookup)
	drawPaths(img, g.paths, g.cx, g.cy, g.zoom, info.Size())
	drawMarkers(img, g.markers, g.cx, g.cy, g.zoom, info.Size())
	drawMapAttribution(img, info.Attribution)
	return composeLetterbox(img, g.cellRectW, g.cellRectH, g.letterbox), missing
}

// jobSlot holds the render job currently allowed to deliver images and
// the background prefetch, if any. It is a pointer on Model so every
// value copy sees the same job and a newer render can cancel both.
type jobSlot struct {
	current  *renderJob
	prefetch context.CancelFunc
}

// cancel stops the current render job and the prefetch. Tiles another
// caller is still waiting for keep downloading.
func (s *jobSlot) cancel() {
	if s == nil {
		return
	}
	if s.current != nil {
		s.current.cancel()
		s.current = nil
	}
	if s.prefetch != nil {
		s.prefetch()
		s.prefetch = nil
	}
}

// renderJob fetches the tiles one render is missing and composes the map
// again as they arrive. start is the Cmd for the first image; each
// partial mapImageMsg carries next, the Cmd that waits for the following
// repaint, so Update keeps a single subscription alive until done.
type renderJob struct {
	ctx    context.Context
	cancel context.CancelFunc
	gen    uint64
	key    renderKey
	loader *tile.Loader
	geom   viewGeometry
	places []tile.Placement

	notify   chan struct{}
	lastEmit time.Time

	mu       sync.Mutex
	finished bool
	fetchErr error
}

func newRenderJob(gen uint64, key renderKey, loader *tile.Loader, geom viewGeometry) *renderJob {
	ctx, cancel := context.WithCancel(context.Background())
	return &renderJob{
		ctx: ctx, cancel: cancel, gen: gen, key: key, loader: loader, geom: geom,
		places: geom.placements(), notify: make(chan struct{}, 1),
	}
}

func (j *renderJob) lookup(id tile.ID) (image.Image, bool) {
	if j.loader == nil {
		return nil, false
	}
	return j.loader.Cached(j.geom.source, id)
}

func (j *renderJob) render() (image.Image, []tile.ID) {
	img, missing := j.geom.compose(j.places, j.lookup)
	yieldToJS()
	return img, missing
}

// start is the render Cmd: it paints what is cached, kicks off the
// fetches for the rest, and returns the first image.
func (j *renderJob) start() tea.Msg {
	img, missing := j.render()
	if len(missing) == 0 || !tile.Fetchable(j.geom.source) || j.loader == nil {
		return mapImageMsg{gen: j.gen, key: j.key, img: img}
	}
	j.lastEmit = time.Now()
	go j.fetch(missing)
	return mapImageMsg{gen: j.gen, key: j.key, img: img, partial: true, next: j.next}
}

func (j *renderJob) fetch(missing []tile.ID) {
	err := j.loader.FetchAll(j.ctx, j.geom.source, missing, func(tile.ID, image.Image) { j.wake() })
	j.mu.Lock()
	j.finished = true
	j.fetchErr = err
	j.mu.Unlock()
	j.wake()
}

func (j *renderJob) wake() {
	select {
	case j.notify <- struct{}{}:
	default:
	}
}

func (j *renderJob) done() (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.finished, j.fetchErr
}

// next waits for more tiles, throttles repaints to progressInterval, and
// returns the next image. A canceled job returns nil, which Bubble Tea
// ignores.
func (j *renderJob) next() tea.Msg {
	select {
	case <-j.notify:
	case <-j.ctx.Done():
		return nil
	}
	if finished, _ := j.done(); !finished {
		if wait := progressInterval - time.Since(j.lastEmit); wait > 0 {
			select {
			case <-time.After(wait):
			case <-j.ctx.Done():
				return nil
			}
		}
	}
	if j.ctx.Err() != nil {
		return nil
	}
	// Drop any wake that arrived while waiting; the compose below sees
	// every tile that caused it.
	select {
	case <-j.notify:
	default:
	}
	finished, err := j.done()
	img, missing := j.render()
	j.lastEmit = time.Now()
	if !finished && len(missing) > 0 {
		return mapImageMsg{gen: j.gen, key: j.key, img: img, partial: true, next: j.next}
	}
	if err != nil && len(missing) == inWorldCount(j.places) {
		// Nothing at all could be shown: surface the error.
		return mapImageMsg{gen: j.gen, key: j.key, err: err}
	}
	return mapImageMsg{gen: j.gen, key: j.key, img: img}
}
