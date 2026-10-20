package ntmaps

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"nimbleterminal.dev/ntmaps/tile"
)

// composeTiles paints the placements into dst, which must already be the
// size Cover was asked for. Tiles lookup cannot supply, and rows outside
// the world, are filled with the provider's placeholder. It returns the
// in-world tiles that were missing, in placement order, so the caller can
// fetch them.
func composeTiles(dst *image.RGBA, placements []tile.Placement, info tile.Info, lookup func(tile.ID) (image.Image, bool)) []tile.ID {
	size := info.Size()
	fill := &image.Uniform{C: info.Fill()}
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

// infoOf returns src's Info, or the zero Info for a nil source.
func infoOf(src tile.Source) tile.Info {
	if src == nil {
		return tile.Info{}
	}
	return src.Info()
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
		drawPin(img, cx+dx-originX, my-originY, size, col)
	}
}

// drawPin rasterizes a map pin with its tip at (x, y): a round head of
// radius size/2 centered size above the tip, joined to the tip by a
// tail, filled with col and outlined in black one pixel wide. Coverage
// comes from the signed distance to the head-or-tail shape, which gives
// anti-aliased edges without a vector library.
func drawPin(img *image.RGBA, x, y, size float64, col color.Color) {
	r := size / 2
	center := point{x, y - size}
	a := point{x - r*math.Sqrt(3)/2, y - size + r/2}
	b := point{x + r*math.Sqrt(3)/2, y - size + r/2}
	tip := point{x, y}

	bounds := img.Bounds()
	x0 := max(bounds.Min.X, int(math.Floor(x-r-2)))
	x1 := min(bounds.Max.X, int(math.Ceil(x+r+2)))
	y0 := max(bounds.Min.Y, int(math.Floor(y-size-r-2)))
	y1 := min(bounds.Max.Y, int(math.Ceil(y+2)))
	if x0 >= x1 || y0 >= y1 {
		return
	}
	if col == nil {
		col = color.RGBA{R: 255, A: 255}
	}
	fill := color.RGBAModel.Convert(col).(color.RGBA)
	black := color.RGBA{A: 255}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			p := point{float64(px) + 0.5, float64(py) + 0.5}
			d := min(sdCircle(p, center, r), sdTriangle(p, a, b, tip))
			outline := clamp01(0.5 - d)
			if outline <= 0 {
				continue
			}
			c := img.RGBAAt(px, py)
			c = blend(c, black, outline)
			c = blend(c, fill, clamp01(-0.5-d))
			img.SetRGBA(px, py, c)
		}
	}
}

type point struct{ x, y float64 }

func (p point) sub(q point) point            { return point{p.x - q.x, p.y - q.y} }
func (p point) scale(s float64) point        { return point{p.x * s, p.y * s} }
func (p point) dot(q point) float64          { return p.x*q.x + p.y*q.y }
func (p point) cross(q point) float64        { return p.x*q.y - p.y*q.x }
func (p point) length() float64              { return math.Hypot(p.x, p.y) }
func clamp01(v float64) float64              { return max(0, min(1, v)) }
func sdCircle(p, c point, r float64) float64 { return p.sub(c).length() - r }

// sdTriangle is the signed distance from p to the triangle abc, negative
// inside (Inigo Quilez's formulation).
func sdTriangle(p, a, b, c point) float64 {
	e0, e1, e2 := b.sub(a), c.sub(b), a.sub(c)
	v0, v1, v2 := p.sub(a), p.sub(b), p.sub(c)
	pq0 := v0.sub(e0.scale(clamp01(v0.dot(e0) / e0.dot(e0))))
	pq1 := v1.sub(e1.scale(clamp01(v1.dot(e1) / e1.dot(e1))))
	pq2 := v2.sub(e2.scale(clamp01(v2.dot(e2) / e2.dot(e2))))
	s := 1.0
	if e0.cross(e2) < 0 {
		s = -1
	}
	dx := min(pq0.dot(pq0), pq1.dot(pq1), pq2.dot(pq2))
	dy := min(s*v0.cross(e0), s*v1.cross(e1), s*v2.cross(e2))
	if dy < 0 {
		return math.Sqrt(dx)
	}
	return -math.Sqrt(dx)
}

// blend returns src over dst with the given coverage, assuming both are
// opaque enough that premultiplication does not matter for the map.
func blend(dst, src color.RGBA, alpha float64) color.RGBA {
	if alpha <= 0 {
		return dst
	}
	if alpha >= 1 {
		return src
	}
	mix := func(d, s uint8) uint8 { return uint8(float64(d)*(1-alpha) + float64(s)*alpha + 0.5) }
	return color.RGBA{R: mix(dst.R, src.R), G: mix(dst.G, src.G), B: mix(dst.B, src.B), A: mix(dst.A, src.A)}
}

// drawMapAttribution draws the provider credit in a translucent strip
// along the bottom edge of img, clipping text that does not fit.
func drawMapAttribution(img *image.RGBA, attribution string) {
	if attribution == "" {
		return
	}
	face := basicfont.Face7x13
	lines := strings.Split(attribution, "\n")
	lineHeight := face.Height
	height := (lineHeight+2)*len(lines) + 2
	b := img.Bounds()
	y := b.Max.Y - height
	draw.Draw(img, image.Rect(b.Min.X, y, b.Max.X, b.Max.Y), &image.Uniform{C: color.NRGBA{A: 128}}, image.Point{}, draw.Over)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.NRGBA{R: 255, G: 255, B: 255, A: 191}), Face: face}
	for _, line := range lines {
		d.Dot = fixed.P(b.Min.X+2, y+face.Ascent)
		d.DrawString(line)
		y += lineHeight + 2
	}
}
