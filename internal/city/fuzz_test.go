package city

import (
	"bytes"
	"testing"
)

// fuzzBounds is a small neighborhood near the equator, where the sample
// sources in the unit tests also live.
var fuzzBounds = Bounds{0, 0, .01, .01}

// maxFuzzSource keeps one input cheap: footprint checks and ear clipping
// are quadratic and cubic in ring size, and a shared fuzzing host runs
// each input on one CPU.
const maxFuzzSource = 32 << 10

// FuzzDecodeBuild decodes arbitrary bytes as OSM JSON and compiles what
// decodes into a scene. A decoder may refuse any input; what it accepts
// must respect the dataset bounds, and every mesh built from it must
// index its own vertices and hold finite geometry.
func FuzzDecodeBuild(f *testing.F) {
	f.Add([]byte(`{"elements":[
 {"type":"node","id":1,"lat":0.001,"lon":0.001},
 {"type":"node","id":2,"lat":0.001,"lon":0.002},
 {"type":"node","id":3,"lat":0.002,"lon":0.001},
 {"type":"way","id":10,"nodes":[1,2,3,1],"tags":{"building":"yes","height":"12 m"}},
 {"type":"way","id":11,"nodes":[1,99,3,1],"tags":{"building":"yes"}},
 {"type":"way","id":12,"nodes":[1,2,3,1],"tags":{"building":"yes","building:levels":"4","roof:levels":"1"}},
 {"type":"way","id":13,"nodes":[1,2],"tags":{"highway":"residential"}},
 {"type":"relation","id":20,"members":[{"type":"way","ref":12}],"tags":{"building":"yes","type":"multipolygon"}}
 ]}`))
	f.Add([]byte(`{"elements":[
 {"type":"way","id":30,"geometry":[{"lat":0.001,"lon":0.001},{"lat":0.001,"lon":0.003},{"lat":0.003,"lon":0.003},{"lat":0.003,"lon":0.001},{"lat":0.001,"lon":0.001}],"tags":{"building":"yes","min_height":"3","height":"40ft"}},
 {"type":"way","id":31,"geometry":[{"lat":0.0015,"lon":0.0015},{"lat":0.0015,"lon":0.002},{"lat":0.002,"lon":0.002},{"lat":0.0015,"lon":0.0015}],"tags":{"building:part":"yes","height":"20"}},
 {"type":"way","id":32,"geometry":[{"lat":-0.5,"lon":0.0},{"lat":0.5,"lon":0.02}],"tags":{"highway":"primary"}}
 ]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"remark":"runtime error: Query timed out","elements":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSource {
			return
		}
		d, err := Decode(bytes.NewReader(data), fuzzBounds)
		if err != nil {
			return
		}
		if len(d.Buildings) > 2000 || len(d.Roads) > 4000 {
			t.Fatalf("%d buildings and %d roads over the source limits", len(d.Buildings), len(d.Roads))
		}
		for _, b := range d.Buildings {
			n := len(b.Footprint)
			if n < 4 || n > 512 || b.Footprint[0] != b.Footprint[n-1] {
				t.Fatalf("building %s kept an open or oversized footprint of %d points", b.ID, n)
			}
			for _, c := range b.Footprint {
				if !c.Valid() {
					t.Fatalf("building %s holds coordinate %v", b.ID, c)
				}
			}
		}
		for i, r := range d.Roads {
			if len(r.Points) < 2 || len(r.Points) > 512 {
				t.Fatalf("road %d has %d points", i, len(r.Points))
			}
			for _, c := range r.Points {
				if !c.Valid() {
					t.Fatalf("road %d holds coordinate %v", i, c)
				}
			}
		}
		s, err := Build(d, DefaultHeightPolicy())
		if err != nil {
			return // over a scene budget
		}
		checkScene(t, s)
	})
}

func checkScene(t *testing.T, s Scene) {
	for _, v := range s.Buildings {
		h := v.Height
		if !finite(h.Top) || !finite(h.Base) || h.Base < 0 || h.Base >= h.Top {
			t.Fatalf("building %s has height %+v", v.ID, h)
		}
		m := v.Mesh
		if len(m.Indices)%3 != 0 {
			t.Fatalf("building %s has %d indices", v.ID, len(m.Indices))
		}
		for _, k := range m.Indices {
			if int(k) >= len(m.Vertices) {
				t.Fatalf("building %s indexes vertex %d of %d", v.ID, k, len(m.Vertices))
			}
		}
		for _, vert := range m.Vertices {
			if !finiteVec(vert.Position) || !finiteVec(vert.Normal) {
				t.Fatalf("building %s has vertex %+v", v.ID, vert)
			}
		}
	}
	for i, r := range s.Roads {
		if len(r) != 2 || !finiteVec(r[0]) || !finiteVec(r[1]) {
			t.Fatalf("road segment %d is %v", i, r)
		}
	}
	for _, issue := range s.Issues {
		if issue.Reason == "" {
			t.Fatalf("issue for %q has no reason", issue.ID)
		}
	}
	if s.Min.X > s.Max.X || s.Min.Y > s.Max.Y || s.Min.Z > s.Max.Z || !finiteVec(s.Min) || !finiteVec(s.Max) {
		t.Fatalf("scene extent %+v to %+v", s.Min, s.Max)
	}
	if k := s.Scale(); !finite(k) || k <= 0 {
		t.Fatalf("scene scale %g", k)
	}
}

func finiteVec(v Vec3) bool { return finite(v.X) && finite(v.Y) && finite(v.Z) }
