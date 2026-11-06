package geodata

import (
	"bytes"
	"math"
	"os"
	"reflect"
	"testing"

	"nimbleterminal.dev/ntmaps"
)

// fuzzLimits keep one input cheap on a shared fuzzing host while leaving
// room for the limits themselves to be exercised.
var fuzzLimits = Limits{Bytes: 1 << 20, Points: 20_000, Features: 2_000}

func seedFile(f *testing.F, name string) {
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
}

// FuzzGeoJSON reads arbitrary bytes as GeoJSON. What is accepted must be
// well-formed map content within the limits, and Read, which tells the
// formats apart by their first byte, must agree with it.
func FuzzGeoJSON(f *testing.F) {
	seedFile(f, "places.geojson")
	f.Add([]byte(`{"type":"Point","coordinates":[-73.989,40.7415]}`))
	f.Add([]byte(`{"type":"Feature","properties":{"name":"Flatiron"},"geometry":{"type":"Polygon","coordinates":[[[-74,40],[-73,40],[-73,41],[-74,40]]]}}`))
	f.Add([]byte(`{"type":"GeometryCollection","geometries":[{"type":"MultiLineString","coordinates":[[[0,0],[1,1]],[[179,0],[-179,0]]]},{"type":"MultiPoint","coordinates":[[1,2,3]]}]}`))
	f.Add([]byte(`{"type":"Polygon","coordinates":[[[0,0],[0,0],[0,0]]]}`)) // closed on arrival with three positions
	f.Add([]byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":null,"properties":{"title":"nowhere"}},{"type":"Feature","geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,0],[1,1],[0,0]],[[0.2,0.2],[0.8,0.2],[0.5,0.5],[0.2,0.2]]]]}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := ReadGeoJSON(bytes.NewReader(data), fuzzLimits)
		if err != nil {
			return
		}
		checkData(t, "geojson", d)
		viaRead, err := Read(bytes.NewReader(data), fuzzLimits)
		if err != nil {
			t.Fatalf("ReadGeoJSON accepted what Read refused: %v", err)
		}
		if !reflect.DeepEqual(d, viaRead) {
			t.Fatalf("Read and ReadGeoJSON disagree:\n%+v\n%+v", d, viaRead)
		}
	})
}

// FuzzGPX reads arbitrary bytes as GPX, with the same checks. Here the
// agreement runs the other way: GPX tolerates text before the root element
// that Read does not, so what Read accepts ReadGPX must accept identically.
func FuzzGPX(f *testing.F) {
	seedFile(f, "walk.gpx")
	f.Add([]byte(`<gpx><wpt lat="40.7415" lon="-73.989"><name>Flatiron</name></wpt></gpx>`))
	f.Add([]byte(`<?xml version="1.0"?><gpx version="1.1"><trk><name>t</name><trkseg><trkpt lat="1" lon="2"/><trkpt lat="1.5" lon="2.5"/></trkseg><trkseg/></trk></gpx>`))
	f.Add([]byte(`<gpx><rte><rtept lat="0" lon="0"/><rtept lat="0" lon="179"/><rtept lat="0" lon="-179"/></rte></gpx>`))
	f.Add([]byte("\ufeff<gpx xmlns=\"http://www.topografix.com/GPX/1/1\"><wpt lat=\" 5 \" lon=\"-5\"/></gpx>"))
	f.Add([]byte(`0<gpx><trk><trkseg><trkpt lat="0" lon="0"/></trkseg></trk></gpx>`)) // one fix; text before the root
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := ReadGPX(bytes.NewReader(data), fuzzLimits)
		if err == nil {
			checkData(t, "gpx", d)
		}
		viaRead, readErr := Read(bytes.NewReader(data), fuzzLimits)
		if readErr != nil || viaRead.Format != "gpx" {
			return
		}
		if err != nil {
			t.Fatalf("Read accepted what ReadGPX refused: %v", err)
		}
		if !reflect.DeepEqual(d, viaRead) {
			t.Fatalf("Read and ReadGPX disagree:\n%+v\n%+v", d, viaRead)
		}
	})
}

// checkData is the oracle shared by the readers. Accepted data stays within
// the limits, keeps no empty feature, holds only valid positions, closes its
// rings, counts its positions, and bounds them all.
func checkData(t *testing.T, format string, d Data) {
	if d.Format != format {
		t.Fatalf("format %q, want %q", d.Format, format)
	}
	if len(d.Features) > fuzzLimits.Features {
		t.Fatalf("%d features over the limit of %d", len(d.Features), fuzzLimits.Features)
	}
	if d.Points > fuzzLimits.Points {
		t.Fatalf("%d points over the limit of %d", d.Points, fuzzLimits.Points)
	}
	positions, rings := 0, 0
	for i, f := range d.Features {
		if len(f.Places)+len(f.Lines)+len(f.Areas) == 0 {
			t.Fatalf("feature %d %q is empty", i, f.Name)
		}
		for _, p := range f.Places {
			checkPoint(t, d, p)
		}
		positions += len(f.Places)
		for _, l := range f.Lines {
			// GeoJSON refuses a line under two positions; GPX keeps a track
			// segment of a single fix, which draws nothing but counts.
			if len(l) < 1 {
				t.Fatalf("feature %d has an empty line", i)
			}
			for _, p := range l {
				checkPoint(t, d, p)
			}
			positions += len(l)
		}
		for _, a := range f.Areas {
			if len(a) == 0 {
				t.Fatalf("feature %d has an area without rings", i)
			}
			for _, ring := range a {
				// The reader asks three positions of a ring and closes it;
				// a ring that arrived closed may hold only three.
				if len(ring) < 3 || ring[0] != ring[len(ring)-1] {
					t.Fatalf("feature %d has an open or short ring: %v", i, ring)
				}
				for _, p := range ring {
					checkPoint(t, d, p)
				}
				positions += len(ring)
				rings++
			}
		}
	}
	// Closing a ring adds a position the reader did not count.
	if positions < d.Points || positions > d.Points+rings {
		t.Fatalf("%d positions in the data, %d counted, %d rings", positions, d.Points, rings)
	}
	if d.Points == 0 && (d.Bounds != ntmaps.Bounds{} || len(d.Features) != 0) {
		t.Fatalf("no positions but bounds %+v and %d features", d.Bounds, len(d.Features))
	}
}

func checkPoint(t *testing.T, d Data, p ntmaps.Point) {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || p.Lat < -90 || p.Lat > 90 || p.Lng < -180 || p.Lng > 180 {
		t.Fatalf("position %v is not a latitude and longitude", p)
	}
	b := d.Bounds
	if p.Lat < b.South || p.Lat > b.North || !containsLng(b, p.Lng) {
		t.Fatalf("position %v outside the bounds %+v", p, b)
	}
}

// containsLng follows ntmaps.Bounds: West above East crosses the antimeridian.
func containsLng(b ntmaps.Bounds, lng float64) bool {
	if b.West <= b.East {
		return lng >= b.West && lng <= b.East
	}
	return lng >= b.West || lng <= b.East
}
