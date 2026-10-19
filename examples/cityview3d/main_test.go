package main

import (
	"github.com/NimbleMarkets/ntcharts-osm/internal/city"
	"testing"
)

func TestBundledLoad(t *testing.T) {
	d, err := load("", false, city.Bounds{})
	if err != nil || len(d.Buildings) == 0 {
		t.Fatalf("bundled load: %v", err)
	}
}
