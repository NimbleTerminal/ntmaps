package cityview

import (
	"image/color"

	chart "github.com/NimbleMarkets/ntcharts3d"
	m3 "nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/ntmaps/internal/city"
)

type citySeries struct{ geometry chart.Geometry }

func (citySeries) Name() string                                     { return "city" }
func (s citySeries) Geometry(chart.Palette) (chart.Geometry, error) { return s.geometry, nil }
func vector(v city.Vec3) m3.Vec3                                    { return m3.Vec3{X: float32(v.X), Y: float32(v.Y), Z: float32(v.Z)} }
func geometry(scene city.Scene) chart.Geometry {
	g := chart.Geometry{LineWidth: 2}
	vertex := func(p, n city.Vec3, c color.RGBA) {
		v := chart.Vertex{Position: vector(p), Normal: vector(n), Color: c}
		g.Vertices = append(g.Vertices, v)
		g.Bounds.Include(v.Position)
	}
	for _, p := range scene.Ground {
		vertex(p, city.Vec3{Z: 1}, color.RGBA{46, 57, 63, 255})
	}
	g.Indices = []uint32{0, 1, 2, 0, 2, 3}
	for _, b := range scene.Buildings {
		offset := uint32(len(g.Vertices))
		for _, v := range b.Mesh.Vertices {
			vertex(v.Position, v.Normal, v.Color)
		}
		for _, i := range b.Mesh.Indices {
			g.Indices = append(g.Indices, offset+i)
		}
	}
	for _, road := range scene.Roads {
		for _, p := range road {
			g.Lines = append(g.Lines, chart.Vertex{Position: vector(p), Color: color.RGBA{99, 119, 130, 255}})
		}
	}
	return g
}

func initialCamera(scene city.Scene) chart.Camera {
	c := chart.DefaultCamera()
	c.Alpha, c.Beta, c.Distance = 38, -65, 3.1
	c.Projection = chart.Perspective
	ground := scene.Normalize(city.Vec3{})
	c.Target = m3.Vec3{X: float32(ground.X), Y: float32(ground.Y), Z: float32(ground.Z + .12)}
	return c
}
