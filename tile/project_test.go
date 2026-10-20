package tile

import (
	"fmt"
	"image"
	"math"
	"testing"
)

func TestProjectUnprojectRoundTrip(t *testing.T) {
	for _, c := range [][2]float64{{0, 0}, {40.7415, -73.989}, {-33.86, 151.21}, {84.9, 179.9}, {-84.9, -179.9}} {
		x, y, _ := Project(c[0], c[1], 12, 256)
		lat, lng := Unproject(x, y, 12, 256)
		if math.Abs(lat-c[0]) > 1e-9 || math.Abs(lng-c[1]) > 1e-9 {
			t.Fatalf("round trip %v → (%f, %f)", c, lat, lng)
		}
	}
}

func TestProjectClampsLatitude(t *testing.T) {
	_, y, _ := Project(89, 0, 0, 256)
	_, yMax, _ := Project(MaxLatitude, 0, 0, 256)
	if y != yMax || math.Signbit(y) || y > 1 {
		t.Fatalf("lat 89 projected to y=%f, want %f", y, yMax)
	}
	if _, y, _ := Project(-89, 0, 0, 256); y != 256 {
		t.Fatalf("lat -89 projected to y=%f, want 256", y)
	}
}

func TestCoverFourTilesAtZoomOne(t *testing.T) {
	cx, cy, _ := Project(0, 0, 1, 256)
	got := Cover(cx, cy, 512, 512, 1, 256)
	want := []Placement{
		{ID{1, 0, 0}, image.Pt(0, 0), true},
		{ID{1, 1, 0}, image.Pt(256, 0), true},
		{ID{1, 0, 1}, image.Pt(0, 256), true},
		{ID{1, 1, 1}, image.Pt(256, 256), true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d placements, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("placement %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCoverCenterPixelIsFloorOfCenter(t *testing.T) {
	// World pixel 300.7 at the center of a 100-px image: pixel 50 shows
	// world pixel 300, so the image spans world 250..349 and tile 1
	// (world 256..511) starts at image pixel 6.
	got := Cover(300.7, 300.7, 100, 100, 2, 256)
	if len(got) != 4 || got[0] != (Placement{ID{2, 0, 0}, image.Pt(-250, -250), true}) || got[3] != (Placement{ID{2, 1, 1}, image.Pt(6, 6), true}) {
		t.Fatalf("unexpected placements %+v", got)
	}
}

func TestCoverWrapsAcrossAntimeridian(t *testing.T) {
	cx, cy, _ := Project(0, 180, 3, 256)
	got := Cover(cx, cy, 300, 100, 3, 256)
	var xs, ys []int
	for _, p := range got {
		if !p.InWorld {
			t.Fatalf("row %d unexpectedly outside the world", p.ID.Y)
		}
		xs = append(xs, p.ID.X)
		ys = append(ys, p.ID.Y)
	}
	if fmt.Sprint(xs) != "[7 0 7 0]" || fmt.Sprint(ys) != "[3 3 4 4]" {
		t.Fatalf("columns = %v rows = %v, want [7 0 7 0] and [3 3 4 4]", xs, ys)
	}
}

func TestCoverMarksRowsOutsideTheWorld(t *testing.T) {
	cx, cy, _ := Project(MaxLatitude, 0, 2, 256)
	got := Cover(cx, cy, 100, 300, 2, 256)
	var outside, inside int
	for _, p := range got {
		if p.InWorld {
			inside++
		} else {
			outside++
			if p.ID.Y >= 0 {
				t.Fatalf("row %d flagged outside but is inside the world", p.ID.Y)
			}
		}
	}
	if outside == 0 || inside == 0 {
		t.Fatalf("expected both inside and outside rows, got %d/%d", inside, outside)
	}
}

func TestCoverRejectsBadInput(t *testing.T) {
	if Cover(0, 0, 0, 10, 1, 256) != nil || Cover(0, 0, 10, 10, -1, 256) != nil {
		t.Fatal("expected nil for empty or invalid views")
	}
}
