package ntmaps

import (
	"context"
	"image"
	"image/color"
	"strings"
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
// at its center, the tile zoom, and the layers to draw.
type viewGeometry struct {
	source   tile.Source
	overlays []tile.Source
	zoom     int // tile zoom, after any oversample boost
	w, h     int // map portion in pixels (before letterbox)
	cx, cy   float64
	markers  []Marker
	paths    []Path

	// cellRectW/H and letterbox pad the map portion to the full cell
	// rectangle; they equal w/h when MaxAspectRatio is off.
	cellRectW, cellRectH int
	letterbox            color.Color
	noAttribution        bool
}

// tileRequest names one tile of one layer that a render still needs.
type tileRequest struct {
	src tile.Source
	id  tile.ID
}

// layers lists the base and every overlay that serves this zoom. Tile
// coordinates are shared across layers at a given zoom, so one set of
// placements serves them all; overlays with another tile size are
// scaled into the base grid when drawn.
func (g viewGeometry) layers() []tile.Source {
	out := []tile.Source{g.source}
	for _, o := range g.overlays {
		if o == nil || g.zoom > o.Info().Max() {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (g viewGeometry) placements() []tile.Placement {
	return tile.Cover(g.cx, g.cy, g.w, g.h, g.zoom, infoOf(g.source).Size())
}

// attribution joins the credits of the base and the overlays in use.
func (g viewGeometry) attribution() string {
	return joinAttribution(g.layers())
}

func joinAttribution(layers []tile.Source) string {
	var parts []string
	for _, l := range layers {
		if a := infoOf(l).Attribution; a != "" {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " · ")
}

// compose paints the base from whatever lookup can supply, draws the
// overlays over it, then paths, markers, and the attribution strip, and
// pads to the cell rectangle. It returns the in-world tiles of every
// layer that were missing, base first.
func (g viewGeometry) compose(placements []tile.Placement, lookup func(tile.Source, tile.ID) (image.Image, bool)) (image.Image, []tileRequest) {
	info := infoOf(g.source)
	img := image.NewRGBA(image.Rect(0, 0, g.w, g.h))
	var missing []tileRequest
	for _, id := range composeTiles(img, placements, info, func(id tile.ID) (image.Image, bool) { return lookup(g.source, id) }) {
		missing = append(missing, tileRequest{g.source, id})
	}
	for _, o := range g.layers()[1:] {
		for _, id := range composeOverlay(img, placements, info.Size(), func(id tile.ID) (image.Image, bool) { return lookup(o, id) }) {
			missing = append(missing, tileRequest{o, id})
		}
	}
	drawPaths(img, g.paths, g.cx, g.cy, g.zoom, info.Size())
	drawMarkers(img, g.markers, g.cx, g.cy, g.zoom, info.Size())
	if !g.noAttribution {
		drawMapAttribution(img, g.attribution())
	}
	return composeLetterbox(img, g.cellRectW, g.cellRectH, g.letterbox), missing
}

// fetchAll fetches every request, layer by layer in parallel, calling
// onTile as tiles land. It returns an error only when no base tile could
// be loaded at all; overlays failing is not an error.
func fetchAll(ctx context.Context, loader *tile.Loader, requests []tileRequest, onTile func()) error {
	// Group by source name: a Source value may hold slices, so it cannot
	// be a map key, and names key the caches anyway.
	type group struct {
		src tile.Source
		ids []tile.ID
	}
	index := map[string]int{}
	var groups []group
	for _, r := range requests {
		name := infoOf(r.src).Name
		i, ok := index[name]
		if !ok {
			i = len(groups)
			index[name] = i
			groups = append(groups, group{src: r.src})
		}
		groups[i].ids = append(groups[i].ids, r.id)
	}
	errs := make([]error, len(groups))
	var wg sync.WaitGroup
	for i, g := range groups {
		if !tile.Fetchable(g.src) {
			continue
		}
		wg.Add(1)
		go func(i int, g group) {
			defer wg.Done()
			errs[i] = loader.FetchAll(ctx, g.src, g.ids, func(tile.ID, image.Image) {
				if onTile != nil {
					onTile()
				}
			})
		}(i, g)
	}
	wg.Wait()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// baseMissing counts the requests that belong to the base layer.
func baseMissing(requests []tileRequest, base tile.Source) int {
	name := infoOf(base).Name
	n := 0
	for _, r := range requests {
		if infoOf(r.src).Name == name {
			n++
		}
	}
	return n
}

// fetchable reports whether any request can be fetched.
func fetchable(requests []tileRequest) bool {
	for _, r := range requests {
		if tile.Fetchable(r.src) {
			return true
		}
	}
	return false
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

func (j *renderJob) lookup(src tile.Source, id tile.ID) (image.Image, bool) {
	if j.loader == nil {
		return nil, false
	}
	return j.loader.Cached(src, id)
}

func (j *renderJob) render() (image.Image, []tileRequest) {
	img, missing := j.geom.compose(j.places, j.lookup)
	yieldToJS()
	return img, missing
}

// start is the render Cmd: it paints what is cached, kicks off the
// fetches for the rest, and returns the first image.
func (j *renderJob) start() tea.Msg {
	img, missing := j.render()
	if len(missing) == 0 || !fetchable(missing) || j.loader == nil {
		return mapImageMsg{gen: j.gen, key: j.key, img: img}
	}
	j.lastEmit = time.Now()
	go j.fetch(missing)
	return mapImageMsg{gen: j.gen, key: j.key, img: img, partial: true, next: j.next}
}

func (j *renderJob) fetch(missing []tileRequest) {
	err := fetchAll(j.ctx, j.loader, missing, j.wake)
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
	if err != nil && baseMissing(missing, j.geom.source) == inWorldCount(j.places) {
		// Nothing at all could be shown: surface the error.
		return mapImageMsg{gen: j.gen, key: j.key, err: err}
	}
	return mapImageMsg{gen: j.gen, key: j.key, img: img}
}
