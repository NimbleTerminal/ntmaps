package tile

import (
	"image"
	"math"
)

// MaxLatitude is the Web Mercator latitude limit. Project clamps to it.
const MaxLatitude = 85.0511287798066

// ID names one tile: zoom level and column/row in the tile grid.
type ID struct{ Z, X, Y int }

// Placement is a tile and where its top-left corner lands in an output
// image. InWorld is false for rows above the north or below the south
// edge of the Mercator square; those tiles do not exist and should be
// filled with the provider's placeholder instead of fetched.
type Placement struct {
	ID      ID
	Offset  image.Point
	InWorld bool
}

// Project converts a coordinate to Web Mercator world pixels at zoom,
// where the world is 2^zoom tiles of tileSize pixels on each side.
func Project(lat, lng float64, zoom, tileSize int) (x, y, world float64) {
	world = math.Exp2(float64(zoom)) * float64(tileSize)
	lat = max(-MaxLatitude, min(MaxLatitude, lat))
	x = (lng + 180) / 360 * world
	y = (1 - math.Asinh(math.Tan(lat*math.Pi/180))/math.Pi) / 2 * world
	y = max(0, min(world, y))
	return
}

// Unproject is the inverse of Project. x wraps around the world; y is
// clamped to it.
func Unproject(x, y float64, zoom, tileSize int) (lat, lng float64) {
	world := math.Exp2(float64(zoom)) * float64(tileSize)
	x = math.Mod(math.Mod(x, world)+world, world)
	y = max(0, min(world, y))
	lng = x/world*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*y/world))) * 180 / math.Pi
	return
}

// Cover lists the tiles needed to fill a w×h image whose pixel (w/2, h/2)
// shows world pixel (⌊cx⌋, ⌊cy⌋), with each tile's offset in that image.
// Columns wrap across the antimeridian; rows outside the world are
// returned with InWorld false so callers can paint a placeholder there.
func Cover(cx, cy float64, w, h, zoom, tileSize int) []Placement {
	if w <= 0 || h <= 0 || tileSize <= 0 || zoom < 0 {
		return nil
	}
	n := 1 << uint(zoom)
	originX := int(math.Floor(cx)) - w/2
	originY := int(math.Floor(cy)) - h/2
	firstX := floorDiv(originX, tileSize)
	lastX := floorDiv(originX+w-1, tileSize)
	firstY := floorDiv(originY, tileSize)
	lastY := floorDiv(originY+h-1, tileSize)

	out := make([]Placement, 0, (lastX-firstX+1)*(lastY-firstY+1))
	for ty := firstY; ty <= lastY; ty++ {
		for tx := firstX; tx <= lastX; tx++ {
			x := ((tx % n) + n) % n
			out = append(out, Placement{
				ID:      ID{Z: zoom, X: x, Y: ty},
				Offset:  image.Pt(tx*tileSize-originX, ty*tileSize-originY),
				InWorld: ty >= 0 && ty < n,
			})
		}
	}
	return out
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}
