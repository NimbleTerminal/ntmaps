package ntmaps

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"

	"github.com/fogleman/gg"

	"nimbleterminal.dev/ntmaps/tile"
)

// composeTiles paints the placements into dst, which must already be the
// size Cover was asked for. Tiles lookup cannot supply, and rows outside
// the world, are filled with the provider's placeholder. It returns the
// in-world tiles that were missing, in placement order, so the caller can
// fetch them.
func composeTiles(dst *image.RGBA, placements []tile.Placement, p tile.Provider, lookup func(tile.ID) (image.Image, bool)) []tile.ID {
	size := p.Size()
	fill := &image.Uniform{C: p.Fill()}
	var missing []tile.ID
	for _, pl := range placements {
		r := image.Rect(pl.Offset.X, pl.Offset.Y, pl.Offset.X+size, pl.Offset.Y+size)
		if pl.InWorld {
			if img, ok := lookup(pl.ID); ok && img != nil {
				draw.Draw(dst, r, img, img.Bounds().Min, draw.Src)
				continue
			}
			missing = append(missing, pl.ID)
		}
		draw.Draw(dst, r, fill, image.Point{}, draw.Src)
	}
	return missing
}

// drawMarkers draws each marker as a pin whose tip is at the marker's
// position. cx, cy are the world pixels of the view center at zoom and
// tileSize, matching the Cover call that produced img. Markers beyond the
// Mercator latitude limit are skipped, longitude uses the world copy
// nearest the center, and pins entirely outside img are not rasterized.
func drawMarkers(img *image.RGBA, markers []Marker, cx, cy float64, zoom, tileSize int) {
	if len(markers) == 0 {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	originX := math.Floor(cx) - float64(w/2)
	originY := math.Floor(cy) - float64(h/2)
	_, _, world := tile.Project(0, 0, zoom, tileSize)
	gc := gg.NewContextForRGBA(img)
	for _, mk := range markers {
		if mk.Lat < -85 || mk.Lat > 85 {
			continue
		}
		col := mk.Color
		if col == nil {
			col = color.RGBA{R: 255, A: 255}
		}
		size := mk.Size
		if size == 0 {
			size = 16
		}
		mx, my, _ := tile.Project(mk.Lat, mk.Lng, zoom, tileSize)
		dx := mx - cx
		dx -= math.Floor(dx/world+0.5) * world
		x := cx + dx - originX
		y := my - originY

		left, top, right, bottom := 0.5*size+1, 1.5*size+1, 0.5*size+1, 1.0
		if x+right < 0 || x-left > float64(w) || y+bottom < 0 || y-top > float64(h) {
			continue
		}
		radius := 0.5 * size
		gc.ClearPath()
		gc.SetLineJoin(gg.LineJoinRound)
		gc.SetLineWidth(1)
		gc.DrawArc(x, y-size, radius, (90+60)*math.Pi/180, (360+90-60)*math.Pi/180)
		gc.LineTo(x, y)
		gc.ClosePath()
		gc.SetColor(col)
		gc.FillPreserve()
		gc.SetRGB(0, 0, 0)
		gc.Stroke()
	}
}

// inWorldCount is the number of placements that name real tiles.
func inWorldCount(placements []tile.Placement) int {
	n := 0
	for _, p := range placements {
		if p.InWorld {
			n++
		}
	}
	return n
}

// renderBuffer composes a w×h map centered on world pixel (cx, cy),
// fetching every missing tile first. It is the non-progressive path used
// by buffered panning, which shows the previous buffer until this one is
// complete. The error is set only when no tile at all could be shown.
func renderBuffer(ctx context.Context, loader *tile.Loader, provider tile.Provider, markers []Marker, cx, cy float64, zoom, w, h int) (image.Image, error) {
	size := provider.Size()
	placements := tile.Cover(cx, cy, w, h, zoom, size)
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	var mu sync.Mutex
	local := make(map[tile.ID]image.Image)
	lookup := func(id tile.ID) (image.Image, bool) {
		mu.Lock()
		t, ok := local[id]
		mu.Unlock()
		if ok {
			return t, true
		}
		if loader != nil {
			return loader.Cached(provider, id)
		}
		return nil, false
	}
	missing := composeTiles(img, placements, provider, lookup)
	if len(missing) > 0 && provider.Valid() && loader != nil {
		err := loader.FetchAll(ctx, provider, missing, func(id tile.ID, t image.Image) {
			mu.Lock()
			local[id] = t
			mu.Unlock()
		})
		if err != nil && len(local) == 0 && len(missing) == inWorldCount(placements) {
			return nil, err
		}
		yieldToJS()
		composeTiles(img, placements, provider, lookup)
	}
	drawMarkers(img, markers, cx, cy, zoom, size)
	return img, nil
}

// drawMapAttribution draws the provider credit in a translucent strip
// along the bottom edge of img.
func drawMapAttribution(img *image.RGBA, attribution string) {
	if attribution == "" {
		return
	}
	gc := gg.NewContextForRGBA(img)
	lines := strings.Split(attribution, "\n")
	var lineHeight float64
	for _, line := range lines {
		_, h := gc.MeasureString(line)
		lineHeight = max(lineHeight, h)
	}
	height := (lineHeight+2)*float64(len(lines)) + 2
	y := float64(img.Bounds().Dy()) - height
	gc.SetRGBA(0, 0, 0, .5)
	gc.DrawRectangle(0, y, float64(img.Bounds().Dx()), height)
	gc.Fill()
	gc.SetRGBA(1, 1, 1, .75)
	for _, line := range lines {
		gc.DrawStringAnchored(line, 2, y, 0, 1)
		y += lineHeight + 2
	}
}
