// Command pmtiles-fetch downloads the tiles covering a bounding box at a
// range of zooms from an XYZ provider and writes them as a PMTiles
// archive, for offline use with ntmaps. Mind the provider's usage
// policy: keep the area and zoom range small.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"nimbleterminal.dev/ntmaps/tile"
	"nimbleterminal.dev/ntmaps/tile/pmtiles"
)

func main() {
	out := flag.String("o", "tiles.pmtiles", "output archive")
	south := flag.Float64("south", 40.738, "south edge")
	west := flag.Float64("west", -73.993, "west edge")
	north := flag.Float64("north", 40.745, "north edge")
	east := flag.Float64("east", -73.985, "east edge")
	minZoom := flag.Int("min-zoom", 12, "lowest zoom")
	maxZoom := flag.Int("max-zoom", 17, "highest zoom")
	name := flag.String("name", "flatiron", "archive name in the metadata")
	url := flag.String("url", "", "tile URL template with {z}/{x}/{y}/{s}; empty means OpenStreetMap")
	ua := flag.String("user-agent", "", "User-Agent; empty means the ntmaps default")
	flag.Parse()

	p := tile.OpenStreetMap()
	if *url != "" {
		p = tile.Provider{Name: *name, URL: *url, Attribution: "see source terms"}
	}
	p.UserAgent = *ua

	tiles := map[tile.ID][]byte{}
	ctx := context.Background()
	for z := *minZoom; z <= *maxZoom; z++ {
		x0, y0, _ := tile.Project(*north, *west, z, 256)
		x1, y1, _ := tile.Project(*south, *east, z, 256)
		for ty := int(math.Floor(y0 / 256)); ty <= int(math.Floor(y1/256)); ty++ {
			for tx := int(math.Floor(x0 / 256)); tx <= int(math.Floor(x1/256)); tx++ {
				id := tile.ID{Z: z, X: tx, Y: ty}
				data, err := p.TileData(ctx, id)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%v: %v\n", id, err)
					os.Exit(1)
				}
				tiles[id] = data
				fmt.Fprintf(os.Stderr, "%v %d bytes\n", id, len(data))
				time.Sleep(150 * time.Millisecond) // stay well under the usage policy
			}
		}
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := pmtiles.Write(f, tiles, pmtiles.WriteOptions{
		Name: *name, Attribution: p.Attribution,
		MinLon: *west, MinLat: *south, MaxLon: *east, MaxLat: *north,
		CenterLon: (*west + *east) / 2, CenterLat: (*south + *north) / 2, CenterZoom: uint8(*maxZoom),
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d tiles to %s\n", len(tiles), *out)
}
