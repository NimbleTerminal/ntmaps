package geodata

import (
	"errors"
	"os"
	"strings"
	"testing"

	"nimbleterminal.dev/ntmaps"
)

func open(t *testing.T, name string) Data {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := Read(f, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGPX(t *testing.T) {
	d := open(t, "walk.gpx")
	if d.Format != "gpx" || len(d.Features) != 4 || d.Points != 9 {
		t.Fatalf("format %q, %d features, %d points", d.Format, len(d.Features), d.Points)
	}
	flatiron, unnamed, route, walk := d.Features[0], d.Features[1], d.Features[2], d.Features[3]
	if flatiron.Name != "Flatiron" || len(flatiron.Places) != 1 || flatiron.Places[0] != (ntmaps.Point{Lat: 40.7411, Lng: -73.9897}) {
		t.Fatalf("waypoint %+v", flatiron)
	}
	if unnamed.Name != "" || len(unnamed.Places) != 1 {
		t.Fatalf("unnamed waypoint %+v", unnamed)
	}
	if route.Name != "Up Fifth" || len(route.Lines) != 1 || len(route.Lines[0]) != 2 {
		t.Fatalf("route %+v", route)
	}
	// One line per segment; the name is trimmed; <metadata><name> is not a feature's.
	if walk.Name != "Morning walk" || len(walk.Lines) != 2 || len(walk.Lines[0]) != 2 || len(walk.Lines[1]) != 3 {
		t.Fatalf("track %+v", walk)
	}
	want := ntmaps.Bounds{South: 40.7359, West: -73.9911, North: 40.7484, East: -73.9857}
	if d.Bounds != want {
		t.Fatalf("bounds %+v, want %+v", d.Bounds, want)
	}
	if len(d.Markers()) != 2 || len(d.Paths()) != 3 {
		t.Fatalf("%d markers, %d paths", len(d.Markers()), len(d.Paths()))
	}
}

func TestGeoJSON(t *testing.T) {
	d := open(t, "places.geojson")
	// The feature with no geometry is skipped.
	if d.Format != "geojson" || len(d.Features) != 4 {
		t.Fatalf("format %q, %d features", d.Format, len(d.Features))
	}
	point, line, area, mixed := d.Features[0], d.Features[1], d.Features[2], d.Features[3]
	if point.Name != "Flatiron" || point.Places[0] != (ntmaps.Point{Lat: 40.7411, Lng: -73.9897}) {
		t.Fatalf("point %+v", point)
	}
	if line.Name != "Broadway" || len(line.Lines) != 1 {
		t.Fatalf("line %+v", line)
	}
	// A name that is not a string is no name; an open ring is closed.
	if area.Name != "" || len(area.Areas) != 1 || len(area.Areas[0]) != 2 {
		t.Fatalf("area %+v", area)
	}
	outer := area.Areas[0][0]
	if len(outer) != 5 || outer[0] != outer[4] {
		t.Fatalf("outer ring not closed: %v", outer)
	}
	if len(mixed.Places) != 2 || len(mixed.Lines) != 1 {
		t.Fatalf("geometry collection %+v", mixed)
	}
	paths := d.Paths()
	if len(paths) != 4 || paths[0].Width != 0 || paths[1].Width != areaWidth || paths[2].Width != areaWidth || paths[3].Width != 0 {
		t.Fatalf("paths %+v", paths)
	}
	if len(d.Markers()) != 3 {
		t.Fatalf("%d markers", len(d.Markers()))
	}
}

func TestBareGeometryAndAntimeridian(t *testing.T) {
	d, err := Read(strings.NewReader(`{"type":"LineString","coordinates":[[179,10],[-179,11]]}`), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// The short way across the antimeridian: West is greater than East.
	if b := d.Bounds; len(d.Features) != 1 || b.West != 179 || b.East != -179 || b.South != 10 || b.North != 11 {
		t.Fatalf("%+v", d)
	}
}

func TestLimits(t *testing.T) {
	gpx := `<gpx><wpt lat="1" lon="2"/><wpt lat="3" lon="4"/><wpt lat="5" lon="6"/></gpx>`
	for _, c := range []struct {
		name string
		l    Limits
	}{
		{"bytes", Limits{Bytes: 20}},
		{"points", Limits{Points: 2}},
		{"features", Limits{Features: 2}},
	} {
		if _, err := Read(strings.NewReader(gpx), c.l); !errors.Is(err, ErrLimit) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if d, err := Read(strings.NewReader(gpx), Limits{Points: 3, Features: 3}); err != nil || d.Points != 3 {
		t.Fatalf("at the limits: %v", err)
	}
	deep := `{"type":"GeometryCollection","geometries":[` + strings.Repeat(`{"type":"GeometryCollection","geometries":[`, maxDepth) +
		strings.Repeat(`]}`, maxDepth) + `]}`
	if _, err := Read(strings.NewReader(deep), Limits{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("deep collections: %v", err)
	}
	if _, err := Read(strings.NewReader("<gpx>"+strings.Repeat("<x>", maxDepth)+strings.Repeat("</x>", maxDepth)+"</gpx>"), Limits{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("deep elements: %v", err)
	}
}

func TestMalformed(t *testing.T) {
	for _, in := range []string{
		``,
		`hello`,
		`{"type":"Point","coordinates":[200,0]}`,
		`{"type":"Point","coordinates":[0,91]}`,
		`{"type":"Point","coordinates":[1]}`,
		`{"type":"Point","coordinates":"x"}`,
		`{"type":"LineString","coordinates":[[0,0]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,1]]]}`,
		`{"type":"Polygon","coordinates":[]}`,
		`{"type":"Circle","coordinates":[0,0]}`,
		`{"coordinates":[0,0]}`,
		`{"type":"FeatureCollection","features":[{"type":"Point","coordinates":[0,0]}]}`,
		`{"type":"Point","coordinates":[0,0]`,
		`<kml></kml>`,
		`<gpx><wpt lat="x" lon="1"/></gpx>`,
		`<gpx><wpt lat="1"/></gpx>`,
		`<gpx><trk><trkseg><trkpt lat="95" lon="0"/></trkseg></trk></gpx>`,
		`<gpx><wpt lat="1" lon="2">`,
	} {
		if d, err := Read(strings.NewReader(in), Limits{}); err == nil {
			t.Errorf("%q read as %+v", in, d)
		}
	}
}

func TestEmptyButValid(t *testing.T) {
	for _, in := range []string{
		"\ufeff  {\"type\":\"FeatureCollection\",\"features\":[]}",
		`<?xml version="1.0"?><gpx version="1.1"></gpx>`,
	} {
		d, err := Read(strings.NewReader(in), Limits{})
		if err != nil || len(d.Features) != 0 || d.Points != 0 || d.Bounds != (ntmaps.Bounds{}) {
			t.Errorf("%q: %+v %v", in, d, err)
		}
	}
}
