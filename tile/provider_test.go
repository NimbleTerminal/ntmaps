package tile

import (
	"testing"
)

func TestTileURLSubstitutesAndShards(t *testing.T) {
	p := Provider{URL: "https://{s}.example/{z}/{x}/{y}.png", Shards: []string{"a", "b", "c"}}
	if got, want := p.TileURL(3, 4, 5), "https://a.example/3/4/5.png"; got != want {
		t.Fatalf("TileURL = %q, want %q", got, want)
	}
	if got, want := p.TileURL(3, 5, 5), "https://b.example/3/5/5.png"; got != want {
		t.Fatalf("TileURL shard = %q, want %q", got, want)
	}
}

func TestTileURLWithoutShards(t *testing.T) {
	p := Wikimedia()
	if got, want := p.TileURL(1, 0, 1), "https://maps.wikimedia.org/osm-intl/1/0/1.png"; got != want {
		t.Fatalf("TileURL = %q, want %q", got, want)
	}
}

func TestArcGISOrdersYBeforeX(t *testing.T) {
	got := ArcGISWorldImagery().TileURL(14, 4823, 6160)
	want := "https://server.arcgisonline.com/arcgis/rest/services/World_Imagery/MapServer/tile/14/6160/4823"
	if got != want {
		t.Fatalf("TileURL = %q, want %q", got, want)
	}
}

func TestEmptyProviderIsInvalid(t *testing.T) {
	var p Provider
	if p.Valid() {
		t.Fatal("empty provider should be invalid")
	}
	if p.TileURL(0, 0, 0) != "" {
		t.Fatal("empty provider should produce no URL")
	}
	if p.Info().Size() != 256 || p.Info().Max() != 19 {
		t.Fatalf("defaults = %d/%d, want 256/19", p.Info().Size(), p.Info().Max())
	}
	if p.Info().Fill() != DefaultPlaceholder {
		t.Fatal("empty provider should use the default placeholder")
	}
}

func TestBuiltinsAreValidAndHTTPS(t *testing.T) {
	for _, p := range []Provider{OpenStreetMap(), Wikimedia(), OpenTopoMap(), CartoLight(), CartoDark(), ArcGISWorldImagery()} {
		if !p.Valid() || p.Name == "" || p.Attribution == "" {
			t.Fatalf("%q: incomplete provider %+v", p.Name, p)
		}
		if u := p.TileURL(1, 1, 1); len(u) < 8 || u[:8] != "https://" {
			t.Fatalf("%q: URL %q is not https", p.Name, u)
		}
	}
	if CartoDark().Info().Fill() == DefaultPlaceholder {
		t.Fatal("dark style should not use the light placeholder")
	}
	if _, _, _, a := CartoDark().Info().Fill().RGBA(); a != 0xffff {
		t.Fatal("placeholder must be opaque")
	}
}
