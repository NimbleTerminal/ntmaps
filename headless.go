package ntmaps

import (
	"context"
	"errors"
	"fmt"
	"image"

	"nimbleterminal.dev/ntmaps/tile"
)

// RenderOptions describes a one-shot map image for Render.
type RenderOptions struct {
	// Source supplies the tiles. nil means tile.OpenStreetMap().
	Source tile.Source

	// Loader caches tiles between calls. nil builds one with the default
	// per-user disk cache; pass your own to share it or to disable disk.
	Loader *tile.Loader

	// Lat, Lng, and Zoom place the view. Zoom must not exceed the source's
	// MaxZoom. When Bounds is set they are derived from it instead.
	Lat, Lng float64
	Zoom     int

	// Bounds, when non-nil, frames this box at the highest zoom that fits
	// inside the image, overriding Lat, Lng, and Zoom.
	Bounds *Bounds

	// Width and Height are the image size in pixels.
	Width, Height int

	Markers []Marker
	Paths   []Path

	// NoAttribution leaves out the credit strip along the bottom edge for
	// callers that credit the source elsewhere. The source's terms still
	// apply.
	NoAttribution bool

	// UserAgent is applied to a tile.Provider that has none. Empty means
	// the ntmaps default; a shipped app should name itself.
	UserAgent string
}

// Render fetches every tile a view needs and composes the map once, with
// no terminal and no Bubble Tea involved: for scripts, documentation
// images, and tests. Tiles that fail to load keep the placeholder; an
// error is returned only when no tile at all could be shown, or when the
// options are invalid. ctx cancels the fetches.
func Render(ctx context.Context, o RenderOptions) (image.Image, error) {
	if o.Width <= 0 || o.Height <= 0 {
		return nil, fmt.Errorf("ntmaps: render size %d×%d must be positive", o.Width, o.Height)
	}
	src := o.Source
	if src == nil {
		src = tile.OpenStreetMap()
	}
	if p, ok := src.(tile.Provider); ok && p.UserAgent == "" && o.UserAgent != "" {
		p.UserAgent = o.UserAgent
		src = p
	}
	info := src.Info()
	if o.Bounds != nil {
		if !o.Bounds.Valid() {
			return nil, fmt.Errorf("ntmaps: invalid bounds %+v", *o.Bounds)
		}
		o.Lat, o.Lng, o.Zoom = fitBounds(*o.Bounds, o.Width, o.Height, info.Size(), info.Max())
	}
	if o.Zoom < 0 || o.Zoom > info.Max() {
		return nil, fmt.Errorf("ntmaps: zoom %d outside 0..%d for %q", o.Zoom, info.Max(), info.Name)
	}
	loader := o.Loader
	if loader == nil {
		loader = tile.NewLoader(tile.Options{Dir: tile.DefaultDir()})
	}

	cx, cy, _ := tile.Project(o.Lat, o.Lng, o.Zoom, info.Size())
	geom := viewGeometry{
		source: src, zoom: o.Zoom, w: o.Width, h: o.Height, cx: cx, cy: cy,
		markers: o.Markers, paths: o.Paths,
		cellRectW: o.Width, cellRectH: o.Height, noAttribution: o.NoAttribution,
	}
	placements := geom.placements()
	lookup := func(id tile.ID) (image.Image, bool) { return loader.Cached(src, id) }
	img, missing := geom.compose(placements, lookup)
	if len(missing) == 0 || !tile.Fetchable(src) {
		return img, nil
	}
	err := loader.FetchAll(ctx, src, missing, nil)
	img, missing = geom.compose(placements, lookup)
	if err != nil && len(missing) == inWorldCount(placements) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if err != nil && errors.Is(err, context.Canceled) {
		return nil, err
	}
	return img, nil
}
