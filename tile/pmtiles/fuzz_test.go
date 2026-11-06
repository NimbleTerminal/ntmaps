package pmtiles

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"unicode"

	"nimbleterminal.dev/ntmaps/tile"
)

// fuzzOptions bound what an archive may ask the reader to allocate, so one
// input cannot stall a shared fuzzing host. The fleet runs each target on a
// single CPU for a few seconds at a time.
var fuzzOptions = Options{MaxDirectoryBytes: 1 << 20, MaxTileBytes: 1 << 20}

// seedArchives are small archives Write produces: one tile, a root holding
// every entry, a root pointing at leaf directories, and duplicate tiles that
// de-duplicate into one blob.
func seedArchives(f *testing.F) [][]byte {
	tiles := map[tile.ID][]byte{}
	for i := range 6 {
		tiles[tile.ID{Z: 3, X: i, Y: 7 - i}] = []byte("tile " + string(rune('a'+i)))
	}
	tiles[tile.ID{Z: 3, X: 7, Y: 7}] = []byte("tile a") // same bytes as 3/0/7
	var seeds [][]byte
	for _, o := range []WriteOptions{
		{Name: "one", Attribution: "© Fixture"},
		{Name: "root", LeafSize: -1},
		{Name: "leaves", LeafSize: 2, TileType: TypeJPEG},
	} {
		var buf bytes.Buffer
		set := tiles
		if o.Name == "one" {
			set = map[tile.ID][]byte{{Z: 0, X: 0, Y: 0}: []byte("world")}
		}
		if err := Write(&buf, set, o); err != nil {
			f.Fatal(err)
		}
		seeds = append(seeds, buf.Bytes())
	}
	return seeds
}

// FuzzArchive opens arbitrary bytes as an archive. A reader may refuse any
// input; what it accepts must honor the read limits, report a printable
// attribution, and serve the tiles its root directory names without
// panicking.
func FuzzArchive(f *testing.F) {
	for _, a := range seedArchives(f) {
		f.Add(a)
	}
	f.Add(make([]byte, HeaderSize))
	f.Add(append([]byte("PMTiles\x03"), make([]byte, HeaderSize-8)...))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := New(bytes.NewReader(data), fuzzOptions)
		if err != nil {
			return
		}
		h := a.Header()
		info := a.Info()
		if info.MaxZoom != int(h.MaxZoom) {
			t.Fatalf("info max zoom %d, header %d", info.MaxZoom, h.MaxZoom)
		}
		checkAttribution(t, info.Attribution)
		if meta, err := a.Metadata(); err == nil && len(meta) > fuzzOptions.MaxDirectoryBytes {
			t.Fatalf("metadata of %d bytes over the %d limit", len(meta), fuzzOptions.MaxDirectoryBytes)
		}

		ids := []tile.ID{{Z: 0}, {Z: 1, X: 1, Y: 1}, {Z: int(h.MinZoom)}, {Z: int(h.MaxZoom)}}
		for _, e := range a.root[:min(len(a.root), 8)] {
			z, x, y := IDToZxy(e.TileID)
			ids = append(ids, tile.ID{Z: int(z), X: int(x), Y: int(y)})
		}
		ctx := context.Background()
		for _, id := range ids {
			data, err := a.TileData(ctx, id)
			if err != nil {
				continue
			}
			if len(data) > fuzzOptions.MaxTileBytes {
				t.Fatalf("tile %v of %d bytes over the %d limit", id, len(data), fuzzOptions.MaxTileBytes)
			}
		}
	})
}

// checkAttribution holds plainAttribution to its promise: one line of
// printable text, bounded in length, since it is drawn on the terminal.
func checkAttribution(t *testing.T, s string) {
	runes := []rune(s)
	if len(runes) > maxAttribution+1 {
		t.Fatalf("attribution of %d runes", len(runes))
	}
	for i, r := range runes {
		if r == ' ' {
			if i == 0 || i == len(runes)-1 || runes[i-1] == ' ' {
				t.Fatalf("attribution %q is not single-spaced", s)
			}
			continue
		}
		if !unicode.IsPrint(r) {
			t.Fatalf("attribution %q holds unprintable %U", s, r)
		}
	}
}

// FuzzDecodeDirectory decodes arbitrary bytes as a directory. What decodes
// must be sorted, no larger than its encoding allows, and survive a round
// trip through EncodeDirectory unchanged.
func FuzzDecodeDirectory(f *testing.F) {
	f.Add(EncodeDirectory(nil))
	f.Add(EncodeDirectory([]Entry{
		{TileID: 0, Offset: 0, Length: 10, RunLength: 1},
		{TileID: 1, Offset: 10, Length: 5, RunLength: 3},
		{TileID: 9, Offset: 100, Length: 7, RunLength: 0},
	}))
	f.Add([]byte{1, 0, 1, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := DecodeDirectory(data)
		if err != nil {
			return
		}
		if len(entries) > len(data)/4 {
			t.Fatalf("%d entries decoded from %d bytes", len(entries), len(data))
		}
		for i := 1; i < len(entries); i++ {
			if entries[i].TileID < entries[i-1].TileID {
				t.Fatalf("entry %d tile id %d below entry %d tile id %d", i, entries[i].TileID, i-1, entries[i-1].TileID)
			}
		}
		again, err := DecodeDirectory(EncodeDirectory(entries))
		if err != nil {
			t.Fatalf("re-encoded directory does not decode: %v", err)
		}
		if !slices.Equal(entries, again) {
			t.Fatalf("round trip changed the directory:\n%v\n%v", entries, again)
		}
	})
}

// FuzzTileID checks the Hilbert tile id against its inverse for every zoom
// the format addresses, and that zooms occupy disjoint, increasing ranges.
func FuzzTileID(f *testing.F) {
	f.Add(uint8(0), uint32(0), uint32(0))
	f.Add(uint8(12), uint32(1205), uint32(1539))
	f.Add(uint8(31), uint32(1<<31-1), uint32(7))
	f.Fuzz(func(t *testing.T, z uint8, x, y uint32) {
		z %= 32
		mask := uint32(uint64(1)<<z - 1)
		x, y = x&mask, y&mask
		id := ZxyToID(z, x, y)
		gz, gx, gy := IDToZxy(id)
		if gz != z || gx != x || gy != y {
			t.Fatalf("%d/%d/%d -> %d -> %d/%d/%d", z, x, y, id, gz, gx, gy)
		}
		if z < 31 && id >= ZxyToID(z+1, 0, 0) {
			t.Fatalf("zoom %d id %d reaches into zoom %d", z, id, z+1)
		}
	})
}

// FuzzWriteRoundTrip writes a small archive and reads every tile back,
// across leaf layouts and de-duplicated contents.
func FuzzWriteRoundTrip(f *testing.F) {
	f.Add(uint8(3), uint32(1), uint32(2), uint8(5), int8(2), []byte("png"))
	f.Add(uint8(0), uint32(0), uint32(0), uint8(0), int8(-1), []byte("world"))
	f.Add(uint8(18), uint32(77000), uint32(98000), uint8(15), int8(1), []byte{0x89, 'P', 'N', 'G'})
	f.Fuzz(func(t *testing.T, z uint8, x, y uint32, n uint8, leafSize int8, payload []byte) {
		z %= 20
		mask := uint32(1)<<z - 1
		if len(payload) == 0 {
			payload = []byte{0} // an encoded raster is never empty
		}
		tiles := map[tile.ID][]byte{}
		for i := range int(n%16) + 1 {
			id := tile.ID{Z: int(z), X: int((x + uint32(i)) & mask), Y: int((y + uint32(i*i)) & mask)}
			data := append([]byte(nil), payload...)
			if i%2 == 1 {
				data = append(data, byte(i)) // half the tiles share bytes, half differ
			}
			tiles[id] = data
		}
		var buf bytes.Buffer
		if err := Write(&buf, tiles, WriteOptions{LeafSize: int(leafSize)}); err != nil {
			t.Fatal(err)
		}
		a, err := New(bytes.NewReader(buf.Bytes()), fuzzOptions)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Header().TileEntries; got != uint64(len(tiles)) {
			t.Fatalf("header counts %d entries for %d tiles", got, len(tiles))
		}
		ctx := context.Background()
		for id, want := range tiles {
			got, err := a.TileData(ctx, id)
			if err != nil {
				t.Fatalf("tile %v: %v", id, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("tile %v read back %q, wrote %q", id, got, want)
			}
		}
		if _, err := a.TileData(ctx, tile.ID{Z: 25}); err != tile.ErrNotFound {
			t.Fatalf("a tile beyond the archive's zooms: %v", err)
		}
	})
}
