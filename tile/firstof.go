package tile

import (
	"context"
	"errors"
	"image"
	"strings"
)

// FirstOf returns a Source that serves each tile from the first of its
// sources that has it, trying the next on ErrNotFound. It is the shape
// of an offline-first setup: a local archive for the area you care about
// in front of an online provider for everything else. Its name joins
// the names, its credit joins the distinct credits, its tile size is the
// first source's, and its max zoom is the largest.
func FirstOf(sources ...Source) Source {
	var kept []Source
	for _, s := range sources {
		if Fetchable(s) {
			kept = append(kept, s)
		}
	}
	switch len(kept) {
	case 0:
		return nil
	case 1:
		return kept[0]
	}
	return firstOf(kept)
}

type firstOf []Source

func (f firstOf) Info() Info {
	first := f[0].Info()
	info := Info{TileSize: first.TileSize, Placeholder: first.Placeholder}
	var names, credits []string
	seen := map[string]bool{}
	for _, s := range f {
		i := s.Info()
		names = append(names, i.Name)
		if i.Attribution != "" && !seen[i.Attribution] {
			seen[i.Attribution] = true
			credits = append(credits, i.Attribution)
		}
		info.MaxZoom = max(info.MaxZoom, i.Max())
	}
	info.Name = strings.Join(names, "+")
	info.Attribution = strings.Join(credits, " · ")
	return info
}

func (f firstOf) Tile(ctx context.Context, id ID) (image.Image, error) {
	var last error
	for _, s := range f {
		img, err := s.Tile(ctx, id)
		if err == nil {
			return img, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// TileData implements DataSource when every source does; otherwise the
// Loader uses Tile and keeps the result in memory only.
func (f firstOf) TileData(ctx context.Context, id ID) ([]byte, error) {
	var last error
	for _, s := range f {
		ds, ok := s.(DataSource)
		if !ok {
			return nil, ErrNoData
		}
		data, err := ds.TileData(ctx, id)
		if err == nil {
			return data, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		last = err
	}
	return nil, last
}

// ErrNoData is returned by a DataSource that cannot supply encoded bytes
// for a tile; Loader then falls back to Tile and keeps the result in
// memory only.
var ErrNoData = errors.New("tile: source has no encoded tiles")
