package city

import (
	"fmt"
	"image/color"
	"math"
)

type Vec3 struct{ X, Y, Z float64 }
type Vertex struct {
	Position, Normal Vec3
	Color            color.RGBA
}
type Mesh struct {
	Vertices []Vertex
	Indices  []uint32
}
type Volume struct {
	ID     string
	Height Height
	Mesh   Mesh
}
type Issue struct{ ID, Reason string }
type Stats struct{ Recorded, Levels, Default, Superseded int }
type Scene struct {
	Projection Projection
	Ground     [4]Vec3
	Buildings  []Volume
	Roads      [][]Vec3
	Issues     []Issue
	Stats      Stats
	Min, Max   Vec3
}

func (s Scene) Scale() float64 {
	return 2 / max(s.Max.X-s.Min.X, s.Max.Y-s.Min.Y, s.Max.Z-s.Min.Z, .001)
}

// Normalize uses the same uniform centering as the viewer's chart adapter.
func (s Scene) Normalize(p Vec3) Vec3 {
	k := s.Scale()
	return Vec3{(p.X - (s.Min.X+s.Max.X)/2) * k, (p.Y - (s.Min.Y+s.Max.Y)/2) * k, (p.Z - (s.Min.Z+s.Max.Z)/2) * k}
}

// Build compiles immutable meshes once, outside camera updates. Explicit heights
// are orange, floor estimates blue, defaults gray. Flat roofs preserve total height.
func Build(d Dataset, policy HeightPolicy) (Scene, error) {
	s := Scene{Projection: Projection{d.Bounds.Center()}}
	if err := d.Bounds.Validate(); err != nil {
		return s, err
	}
	if err := policy.Validate(); err != nil {
		return s, err
	}
	if len(d.Buildings) > 2000 {
		return s, fmt.Errorf("city: too many buildings")
	}
	if len(d.Roads) > 4000 {
		return s, fmt.Errorf("city: too many roads")
	}
	west, south := s.Projection.Project(Coordinate{d.Bounds.South, d.Bounds.West})
	east, north := s.Projection.Project(Coordinate{d.Bounds.North, d.Bounds.East})
	s.Ground = [4]Vec3{{west, south, -.1}, {east, south, -.1}, {east, north, -.1}, {west, north, -.1}}
	s.Min, s.Max = Vec3{west, south, -.1}, Vec3{east, north, 0}
	rings := make([][]Vec3, len(d.Buildings))
	for i, b := range d.Buildings {
		ring, err := footprint(b.Footprint, s.Projection)
		if err != nil {
			s.Issues = append(s.Issues, Issue{b.ID, err.Error()})
			continue
		}
		rings[i] = ring
	}
	triangles := 0
	for i, b := range d.Buildings {
		ring := rings[i]
		if ring == nil {
			continue
		}
		if !b.Part {
			hasParts := false
			for j, part := range d.Buildings {
				if !part.Part || rings[j] == nil {
					continue
				}
				inside := true
				for _, p := range rings[j] {
					if !contains(ring, p) {
						inside = false
						break
					}
				}
				if inside {
					hasParts = true
					break
				}
			}
			if hasParts {
				s.Stats.Superseded++
				continue
			}
		}
		indices, err := triangulate(ring)
		if err != nil {
			s.Issues = append(s.Issues, Issue{b.ID, err.Error()})
			continue
		}
		h := policy.Resolve(b.Tags)
		c := color.RGBA{151, 161, 172, 255}
		switch h.Source {
		case HeightTag:
			s.Stats.Recorded++
			c = color.RGBA{222, 174, 111, 255}
		case HeightLevels:
			s.Stats.Levels++
			c = color.RGBA{111, 173, 205, 255}
		default:
			s.Stats.Default++
		}
		mesh := extrude(ring, indices, h, c)
		triangles += len(mesh.Indices) / 3
		if triangles > 200000 {
			return Scene{}, fmt.Errorf("city: scene exceeds 200000 triangles")
		}
		for _, v := range mesh.Vertices {
			p := v.Position
			s.Min = Vec3{min(s.Min.X, p.X), min(s.Min.Y, p.Y), min(s.Min.Z, p.Z)}
			s.Max = Vec3{max(s.Max.X, p.X), max(s.Max.Y, p.Y), max(s.Max.Z, p.Z)}
		}
		s.Buildings = append(s.Buildings, Volume{b.ID, h, mesh})
	}
	for _, road := range d.Roads {
		if len(road.Points) > 512 {
			return Scene{}, fmt.Errorf("city: road exceeds 512 points")
		}
		for i := 1; i < len(road.Points); i++ {
			a, b := road.Points[i-1], road.Points[i]
			if !a.Valid() || !b.Valid() {
				continue
			}
			ax, ay := s.Projection.Project(a)
			bx, by := s.Projection.Project(b)
			if a, b, ok := clipLine(Vec3{ax, ay, 0}, Vec3{bx, by, 0}, west, south, east, north); ok {
				if len(s.Roads) >= 50000 {
					return Scene{}, fmt.Errorf("city: scene exceeds 50000 road segments")
				}
				s.Roads = append(s.Roads, []Vec3{a, b})
			}
		}
	}
	return s, nil
}
func cross(a, b, c Vec3) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
func onSegment(a, b, p Vec3) bool {
	return math.Abs(cross(a, b, p)) < 1e-8 && p.X >= min(a.X, b.X)-1e-8 && p.X <= max(a.X, b.X)+1e-8 && p.Y >= min(a.Y, b.Y)-1e-8 && p.Y <= max(a.Y, b.Y)+1e-8
}
func intersects(a, b, c, d Vec3) bool {
	x, y, z, w := cross(a, b, c), cross(a, b, d), cross(c, d, a), cross(c, d, b)
	return (x*y < 0 && z*w < 0) || onSegment(a, b, c) || onSegment(a, b, d) || onSegment(c, d, a) || onSegment(c, d, b)
}
func contains(r []Vec3, p Vec3) bool {
	in := false
	for i, a := range r {
		b := r[(i+1)%len(r)]
		if onSegment(a, b, p) {
			return true
		}
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}
func footprint(points []Coordinate, p Projection) ([]Vec3, error) {
	if len(points) < 4 || len(points) > 512 || points[0] != points[len(points)-1] {
		return nil, fmt.Errorf("unsupported open/oversized ring")
	}
	r := make([]Vec3, 0, len(points)-1)
	for _, c := range points[:len(points)-1] {
		if !c.Valid() {
			return nil, fmt.Errorf("invalid coordinate")
		}
		x, y := p.Project(c)
		v := Vec3{x, y, 0}
		if len(r) == 0 || r[len(r)-1] != v {
			r = append(r, v)
		}
	}
	if len(r) > 1 && r[0] == r[len(r)-1] {
		r = r[:len(r)-1]
	}
	for changed := true; changed && len(r) > 3; {
		changed = false
		for i := range r {
			a, b, c := r[(i+len(r)-1)%len(r)], r[i], r[(i+1)%len(r)]
			if onSegment(a, c, b) {
				r = append(r[:i], r[i+1:]...)
				changed = true
				break
			}
		}
	}
	if len(r) < 3 {
		return nil, fmt.Errorf("degenerate footprint")
	}
	area := 0.0
	for i, a := range r {
		b := r[(i+1)%len(r)]
		area += a.X*b.Y - a.Y*b.X
		for j := i + 1; j < len(r); j++ {
			if j == i+1 || (i == 0 && j == len(r)-1) {
				continue
			}
			if intersects(a, b, r[j], r[(j+1)%len(r)]) {
				return nil, fmt.Errorf("self-intersecting footprint")
			}
		}
	}
	if math.Abs(area) < .01 {
		return nil, fmt.Errorf("degenerate footprint")
	}
	if area < 0 {
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
	}
	return r, nil
}
func triangulate(r []Vec3) ([]uint32, error) {
	remaining := make([]int, len(r))
	for i := range remaining {
		remaining[i] = i
	}
	out := make([]uint32, 0, 3*(len(r)-2))
	for len(remaining) > 3 {
		found := false
		for i, b := range remaining {
			a, c := remaining[(i+len(remaining)-1)%len(remaining)], remaining[(i+1)%len(remaining)]
			if cross(r[a], r[b], r[c]) <= 1e-8 {
				continue
			}
			blocked := false
			for _, j := range remaining {
				if j == a || j == b || j == c {
					continue
				}
				if cross(r[a], r[b], r[j]) >= -1e-8 && cross(r[b], r[c], r[j]) >= -1e-8 && cross(r[c], r[a], r[j]) >= -1e-8 {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			out = append(out, uint32(a), uint32(b), uint32(c))
			remaining = append(remaining[:i], remaining[i+1:]...)
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("cannot triangulate footprint")
		}
	}
	return append(out, uint32(remaining[0]), uint32(remaining[1]), uint32(remaining[2])), nil
}
func extrude(r []Vec3, roof []uint32, h Height, c color.RGBA) Mesh {
	m := Mesh{}
	for _, z := range []float64{h.Top, h.Base} {
		normal := Vec3{Z: 1}
		if z == h.Base {
			normal.Z = -1
		}
		for _, p := range r {
			p.Z = z
			m.Vertices = append(m.Vertices, Vertex{p, normal, c})
		}
	}
	m.Indices = append(m.Indices, roof...)
	n := uint32(len(r))
	for i := 0; i < len(roof); i += 3 {
		m.Indices = append(m.Indices, roof[i+2]+n, roof[i+1]+n, roof[i]+n)
	}
	for i, a := range r {
		b := r[(i+1)%len(r)]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math.Hypot(dx, dy)
		normal := Vec3{dy / l, -dx / l, 0}
		o := uint32(len(m.Vertices))
		for _, p := range []Vec3{{a.X, a.Y, h.Base}, {b.X, b.Y, h.Base}, {b.X, b.Y, h.Top}, {a.X, a.Y, h.Top}} {
			m.Vertices = append(m.Vertices, Vertex{p, normal, c})
		}
		m.Indices = append(m.Indices, o, o+1, o+2, o, o+2, o+3)
	}
	return m
}
func clipLine(a, b Vec3, w, s, e, n float64) (Vec3, Vec3, bool) {
	dx, dy := b.X-a.X, b.Y-a.Y
	t0, t1 := 0.0, 1.0
	for _, v := range [][2]float64{{-dx, a.X - w}, {dx, e - a.X}, {-dy, a.Y - s}, {dy, n - a.Y}} {
		p, q := v[0], v[1]
		if p == 0 {
			if q < 0 {
				return a, b, false
			}
			continue
		}
		t := q / p
		if p < 0 {
			t0 = max(t0, t)
		} else {
			t1 = min(t1, t)
		}
	}
	if t0 > t1 {
		return a, b, false
	}
	return Vec3{a.X + t0*dx, a.Y + t0*dy, 0}, Vec3{a.X + t1*dx, a.Y + t1*dy, 0}, true
}
