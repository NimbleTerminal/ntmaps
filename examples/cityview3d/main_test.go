package main

import (
	"nimbleterminal.dev/ntmaps/internal/city"
	"testing"
)

func TestBundledLoad(t *testing.T) {
	d, err := load("", false, city.Bounds{})
	if err != nil || len(d.Buildings) == 0 {
		t.Fatalf("bundled load: %v", err)
	}
}
