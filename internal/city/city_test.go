package city

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeightSelectionAndUnits(t *testing.T) {
	for _, tt := range []struct {
		name      string
		tags      map[string]string
		top, base float64
		source    HeightSource
	}{
		{"explicit", map[string]string{"height": "24 m", "building:levels": "3"}, 24, 0, HeightTag},
		{"feet", map[string]string{"height": "100 ft", "min_height": "10 feet"}, 30.48, 3.048, HeightTag},
		{"floors", map[string]string{"building:levels": "5", "roof:height": "2"}, 17, 0, HeightLevels},
		{"roof floors", map[string]string{"building:levels": "5", "roof:levels": "1"}, 18, 0, HeightLevels},
		{"invalid explicit", map[string]string{"height": "NaN", "building:levels": "4", "building:min_level": "1"}, 12, 3, HeightLevels},
		{"invalid everything", map[string]string{"height": "Inf", "building:levels": "-2"}, 9, 0, HeightDefault},
		{"inverted", map[string]string{"height": "5", "min_height": "10"}, 5, 0, HeightTag},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultHeightPolicy().Resolve(tt.tags)
			if math.Abs(got.Top-tt.top) > 1e-6 || math.Abs(got.Base-tt.base) > 1e-6 || got.Source != tt.source {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
func TestProjectionPreservesLocalMeterScale(t *testing.T) {
	for _, origin := range []Coordinate{{0, 0}, {40.74, -73.99}, {-33.9, 151.2}} {
		b, err := Around(origin, 100)
		if err != nil {
			t.Fatal(err)
		}
		p := Projection{origin}
		x, y := p.Project(origin)
		if math.Abs(x)+math.Abs(y) > 1e-6 {
			t.Fatal("origin displaced")
		}
		x, y = p.Project(Coordinate{b.North, b.East})
		if math.Abs(x-100) > .02 || math.Abs(y-100) > .02 {
			t.Fatalf("meter scale lost: %g %g", x, y)
		}
	}
	if _, err := Around(Coordinate{Lat: 90}, 100); err == nil {
		t.Fatal("polar origin accepted")
	}
}
func TestConcaveMeshWindingAndNormals(t *testing.T) {
	// A 10x10 L with a 6x6 corner removed. Both source windings must produce
	// exactly the footprint's area on the roof, with no triangles filling the notch.
	input := []Vec3{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 4}, {X: 4, Y: 4}, {X: 4, Y: 10}, {X: 0, Y: 10}}
	for _, reverse := range []bool{false, true} {
		points := make([]Coordinate, 0, len(input)+1)
		for i := range input {
			j := i
			if reverse {
				j = len(input) - 1 - i
			}
			v := input[j]
			points = append(points, Coordinate{Lat: v.Y / earthRadius * 180 / math.Pi, Lon: v.X / earthRadius * 180 / math.Pi})
		}
		points = append(points, points[0])
		ring, err := footprint(points, Projection{})
		if err != nil {
			t.Fatal(err)
		}
		roof, err := triangulate(ring)
		if err != nil {
			t.Fatal(err)
		}
		area := 0.0
		for i := 0; i < len(roof); i += 3 {
			a, b, c := ring[roof[i]], ring[roof[i+1]], ring[roof[i+2]]
			cross := cross(a, b, c)
			if cross <= 0 {
				t.Fatal("roof faces inward")
			}
			area += cross / 2
			mid := Vec3{X: (a.X + b.X + c.X) / 3, Y: (a.Y + b.Y + c.Y) / 3}
			if !contains(ring, mid) {
				t.Fatal("roof escaped footprint")
			}
		}
		if math.Abs(area-64) > .001 {
			t.Fatalf("roof area = %g", area)
		}
		mesh := extrude(ring, roof, Height{Top: 20, Base: 3}, color.RGBA{255, 255, 255, 255})
		for _, v := range mesh.Vertices {
			if v.Position.Z != 3 && v.Position.Z != 20 {
				t.Fatal("incorrect height")
			}
			if math.Abs(math.Sqrt(v.Normal.X*v.Normal.X+v.Normal.Y*v.Normal.Y+v.Normal.Z*v.Normal.Z)-1) > 1e-6 {
				t.Fatal("invalid normal")
			}
		}
		for _, i := range mesh.Indices {
			if int(i) >= len(mesh.Vertices) {
				t.Fatal("index outside vertices")
			}
		}
	}
}

func TestDecodeAndRejectUnsupportedSource(t *testing.T) {
	bounds := Bounds{0, 0, .01, .01}
	source := `{"elements":[
 {"type":"node","id":1,"lat":0.001,"lon":0.001},
 {"type":"node","id":2,"lat":0.001,"lon":0.002},
 {"type":"node","id":3,"lat":0.002,"lon":0.001},
 {"type":"way","id":10,"nodes":[1,2,3,1],"tags":{"building":"yes","height":"12"}},
 {"type":"way","id":11,"nodes":[1,99,3,1],"tags":{"building":"yes"}},
 {"type":"way","id":12,"nodes":[1,2,3,1],"tags":{"building":"yes"}},
 {"type":"relation","id":20,"members":[{"type":"way","ref":12}],"tags":{"building":"yes","type":"multipolygon"}}
 ]}`
	d, err := Decode(strings.NewReader(source), bounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Buildings) != 1 || d.Buildings[0].ID != "way/10" || d.SkippedRelations != 1 || d.SkippedWays != 2 {
		t.Fatalf("unexpected decode: %+v", d)
	}
	for _, bad := range []string{`{}`, `{"remark":"query timed out","elements":[]}`, `{"elements":`} {
		_, err := Decode(strings.NewReader(bad), bounds)
		// Empty documents are valid but contain no buildings; partial queries aren't.
		if bad != "{}" && err == nil {
			t.Fatal("incomplete source accepted")
		}
	}
	scene, err := Build(d, DefaultHeightPolicy())
	if err != nil || len(scene.Buildings) != 1 {
		t.Fatalf("build: %v %+v", err, scene.Issues)
	}
	if scene.Buildings[0].Height.Top != 12 {
		t.Fatal("lost source height")
	}
}
func TestInvalidRingAndPartPrecedence(t *testing.T) {
	bounds := Bounds{0, 0, .01, .01}
	ring := []Coordinate{{.001, .001}, {.001, .003}, {.003, .003}, {.003, .001}, {.001, .001}}
	part := []Coordinate{{.0015, .0015}, {.0015, .002}, {.002, .002}, {.002, .0015}, {.0015, .0015}}
	bowtie := []Coordinate{{.001, .001}, {.003, .003}, {.003, .001}, {.001, .003}, {.001, .001}}
	d := Dataset{Bounds: bounds, Buildings: []Building{{ID: "outline", Footprint: ring}, {ID: "part", Footprint: part, Part: true, Tags: map[string]string{"height": "20"}}, {ID: "invalid", Footprint: bowtie}}}
	scene, err := Build(d, DefaultHeightPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Buildings) != 1 || scene.Buildings[0].ID != "part" || scene.Stats.Superseded != 1 || len(scene.Issues) != 1 {
		t.Fatalf("unexpected precedence %+v %+v", scene.Stats, scene.Issues)
	}
	if _, err = Build(d, HeightPolicy{MetersPerLevel: math.NaN()}); err == nil {
		t.Fatal("invalid height policy accepted")
	}
}
func TestClientLimitsAndCancellation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("bbox") == "" || r.Header.Get("User-Agent") == "" {
			t.Error("missing request metadata")
		}
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), Endpoint: server.URL}
	b, _ := Around(Coordinate{40, -74}, 100)
	if _, err := client.Fetch(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Fetch(context.Background(), Bounds{0, 0, 1, 1}); err == nil || requests != 1 {
		t.Fatal("oversized area reached server")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Fetch(ctx, b); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}

func TestProjectionRoundTrip(t *testing.T) {
	p := Projection{Origin: Coordinate{40.7415, -73.989}}
	for _, c := range []Coordinate{{40.7415, -73.989}, {40.742, -73.988}, {40.740, -73.990}} {
		x, y := p.Project(c)
		back := p.Unproject(x, y)
		if math.Abs(c.Lat-back.Lat) > 1e-9 || math.Abs(c.Lon-back.Lon) > 1e-9 {
			t.Fatalf("round trip %+v -> %+v", c, back)
		}
	}
}
