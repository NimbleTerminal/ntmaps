package pmtiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"nimbleterminal.dev/ntmaps/tile"
)

// Flatiron, Manhattan: the area the bundled 3D neighborhood covers.
const (
	flatironLat = 40.7415
	flatironLng = -73.989
)

// tileColor encodes a tile's coordinates in a color so a lookup can be
// checked by pixel.
func tileColor(id tile.ID) color.RGBA {
	return color.RGBA{R: uint8(id.Z * 16), G: uint8(id.X % 251), B: uint8(id.Y % 251), A: 255}
}

func pngTile(c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// flatironTiles returns a 3×3 block of tiles around Flatiron at zooms
// 10 through 12.
func flatironTiles() map[tile.ID][]byte {
	tiles := map[tile.ID][]byte{}
	for z := 10; z <= 12; z++ {
		x, y, _ := tile.Project(flatironLat, flatironLng, z, 256)
		cx, cy := int(math.Floor(x/256)), int(math.Floor(y/256))
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				id := tile.ID{Z: z, X: cx + dx, Y: cy + dy}
				tiles[id] = pngTile(tileColor(id))
			}
		}
	}
	return tiles
}

type fixture struct {
	leafSize     int   // entries per leaf; 0 keeps everything in the root
	compression  uint8 // internal compression
	tileType     uint8
	tileCompress uint8
}

func compress(data []byte, c uint8) []byte {
	if c != CompressionGzip {
		return data
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

// buildArchive writes a v3 archive holding tiles.
func buildArchive(t *testing.T, tiles map[tile.ID][]byte, f fixture) []byte {
	t.Helper()
	if f.compression == 0 {
		f.compression = CompressionGzip
	}
	if f.tileType == 0 {
		f.tileType = TypePNG
	}
	if f.tileCompress == 0 {
		f.tileCompress = CompressionNone
	}
	type keyed struct {
		id   uint64
		data []byte
	}
	var all []keyed
	minZ, maxZ := 255, 0
	for id, data := range tiles {
		all = append(all, keyed{ZxyToID(uint8(id.Z), uint32(id.X), uint32(id.Y)), compress(data, f.tileCompress)})
		minZ, maxZ = min(minZ, id.Z), max(maxZ, id.Z)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	var tileData []byte
	var entries []Entry
	for _, k := range all {
		entries = append(entries, Entry{TileID: k.id, Offset: uint64(len(tileData)), Length: uint32(len(k.data)), RunLength: 1})
		tileData = append(tileData, k.data...)
	}

	var root []Entry
	var leaves []byte
	if f.leafSize > 0 && len(entries) > f.leafSize {
		for i := 0; i < len(entries); i += f.leafSize {
			chunk := entries[i:min(i+f.leafSize, len(entries))]
			leaf := compress(EncodeDirectory(chunk), f.compression)
			root = append(root, Entry{TileID: chunk[0].TileID, Offset: uint64(len(leaves)), Length: uint32(len(leaf))})
			leaves = append(leaves, leaf...)
		}
	} else {
		root = entries
	}
	rootBytes := compress(EncodeDirectory(root), f.compression)
	meta := compress([]byte(`{"attribution":"Fixture (c) test","name":"flatiron"}`), f.compression)

	h := make([]byte, HeaderSize)
	copy(h, magic)
	h[7] = 3
	put := func(off int, v uint64) { binary.LittleEndian.PutUint64(h[off:], v) }
	e7 := func(off int, v float64) { binary.LittleEndian.PutUint32(h[off:], uint32(int32(math.Round(v*1e7)))) }
	rootOff := uint64(HeaderSize)
	metaOff := rootOff + uint64(len(rootBytes))
	leafOff := metaOff + uint64(len(meta))
	dataOff := leafOff + uint64(len(leaves))
	put(8, rootOff)
	put(16, uint64(len(rootBytes)))
	put(24, metaOff)
	put(32, uint64(len(meta)))
	put(40, leafOff)
	put(48, uint64(len(leaves)))
	put(56, dataOff)
	put(64, uint64(len(tileData)))
	put(72, uint64(len(entries)))
	put(80, uint64(len(entries)))
	put(88, uint64(len(entries)))
	h[96] = 1
	h[97] = f.compression
	h[98] = f.tileCompress
	h[99] = f.tileType
	h[100] = uint8(minZ)
	h[101] = uint8(maxZ)
	e7(102, flatironLng-0.05)
	e7(106, flatironLat-0.05)
	e7(110, flatironLng+0.05)
	e7(114, flatironLat+0.05)
	h[118] = 12
	e7(119, flatironLng)
	e7(123, flatironLat)

	var out []byte
	out = append(out, h...)
	out = append(out, rootBytes...)
	out = append(out, meta...)
	out = append(out, leaves...)
	out = append(out, tileData...)
	return out
}

func checkTile(t *testing.T, a *Archive, id tile.ID) {
	t.Helper()
	img, err := a.Tile(context.Background(), id)
	if err != nil {
		t.Fatalf("tile %v: %v", id, err)
	}
	if got := color.RGBAModel.Convert(img.At(5, 5)).(color.RGBA); got != tileColor(id) {
		t.Fatalf("tile %v = %v, want %v", id, got, tileColor(id))
	}
}

func TestZxyToIDMatchesTheSpecification(t *testing.T) {
	for _, c := range []struct {
		z    uint8
		x, y uint32
		id   uint64
	}{{0, 0, 0, 0}, {1, 0, 0, 1}, {1, 0, 1, 2}, {1, 1, 1, 3}, {1, 1, 0, 4}, {2, 0, 0, 5}} {
		if got := ZxyToID(c.z, c.x, c.y); got != c.id {
			t.Fatalf("ZxyToID(%d,%d,%d) = %d, want %d", c.z, c.x, c.y, got, c.id)
		}
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		z := uint8(r.Intn(20))
		n := uint32(1) << z
		x, y := uint32(r.Int63n(int64(n))), uint32(r.Int63n(int64(n)))
		gz, gx, gy := IDToZxy(ZxyToID(z, x, y))
		if gz != z || gx != x || gy != y {
			t.Fatalf("round trip %d/%d/%d → %d/%d/%d", z, x, y, gz, gx, gy)
		}
	}
	// Neighbors along the curve keep nearby IDs within a zoom.
	if ZxyToID(12, 1206, 1540) <= ZxyToID(11, 2047, 2047) {
		t.Fatal("zoom 12 ids must follow every zoom 11 id")
	}
}

func TestArchiveServesTilesFromAFile(t *testing.T) {
	tiles := flatironTiles()
	path := filepath.Join(t.TempDir(), "flatiron.pmtiles")
	if err := os.WriteFile(path, buildArchive(t, tiles, fixture{leafSize: 8}), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	info := a.Info()
	if info.Name != "flatiron" || info.Attribution != "Fixture (c) test" || info.Max() != 12 || info.Size() != 256 {
		t.Fatalf("info = %+v", info)
	}
	if h := a.Header(); h.MinZoom != 10 || h.MaxZoom != 12 || !h.Clustered || math.Abs(h.CenterLat-flatironLat) > 1e-6 {
		t.Fatalf("header = %+v", h)
	}
	if s, w, n, e := a.Bounds(); s >= flatironLat || n <= flatironLat || w >= flatironLng || e <= flatironLng {
		t.Fatalf("bounds %g,%g,%g,%g do not contain Flatiron", s, w, n, e)
	}
	if len(a.root) >= len(tiles) {
		t.Fatalf("fixture should use leaf directories, root has %d entries", len(a.root))
	}
	for id := range tiles {
		checkTile(t, a, id)
	}
	x, y, _ := tile.Project(flatironLat, flatironLng, 12, 256)
	cx, cy := int(math.Floor(x/256)), int(math.Floor(y/256))
	for _, id := range []tile.ID{{Z: 12, X: cx + 5, Y: cy}, {Z: 9, X: cx >> 3, Y: cy >> 3}, {Z: 13, X: cx * 2, Y: cy * 2}, {Z: 12, X: -1, Y: 0}} {
		if _, err := a.Tile(context.Background(), id); !errors.Is(err, tile.ErrNotFound) {
			t.Fatalf("tile %v: got %v, want ErrNotFound", id, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.TileData(ctx, tile.ID{Z: 12, X: cx, Y: cy}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
	if !tile.Fetchable(a) {
		t.Fatal("an archive is fetchable")
	}
}

func TestArchiveWithoutLeavesOrCompression(t *testing.T) {
	tiles := flatironTiles()
	a, err := New(bytes.NewReader(buildArchive(t, tiles, fixture{compression: CompressionNone, tileCompress: CompressionGzip})), Options{Name: "raw", Attribution: "Mine", TileSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.root) != len(tiles) {
		t.Fatalf("root has %d entries, want %d", len(a.root), len(tiles))
	}
	if info := a.Info(); info.Name != "raw" || info.Attribution != "Mine" || info.Size() != 512 {
		t.Fatalf("info = %+v", info)
	}
	for id := range tiles {
		checkTile(t, a, id)
	}
}

func TestArchiveThroughTheLoader(t *testing.T) {
	tiles := flatironTiles()
	a, err := New(bytes.NewReader(buildArchive(t, tiles, fixture{leafSize: 4})), Options{Name: "fx"})
	if err != nil {
		t.Fatal(err)
	}
	l := tile.NewLoader(tile.Options{Dir: t.TempDir()})
	var id tile.ID
	for id = range tiles {
		break
	}
	img, err := l.Fetch(context.Background(), a, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.RGBAModel.Convert(img.At(1, 1)).(color.RGBA); got != tileColor(id) {
		t.Fatalf("loader tile = %v", got)
	}
	if _, ok := l.Cached(a, id); !ok {
		t.Fatal("tile should be cached")
	}
	var ids []tile.ID
	for id := range tiles {
		ids = append(ids, id)
	}
	n := 0
	if err := l.FetchAll(context.Background(), a, ids, func(tile.ID, image.Image) { n++ }); err != nil || n != len(ids) {
		t.Fatalf("FetchAll: err=%v n=%d", err, n)
	}
}

func TestArchiveOverHTTPRangeRequests(t *testing.T) {
	data := buildArchive(t, flatironTiles(), fixture{leafSize: 8})
	var ua string
	ranged := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		http.ServeContent(w, r, "flatiron.pmtiles", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(ranged.Close)
	a, err := OpenURL(ranged.URL+"/tiles/flatiron.pmtiles", nil, "ntmaps-test/1.0", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Info().Name != "flatiron" {
		t.Fatalf("name = %q", a.Info().Name)
	}
	for id := range flatironTiles() {
		checkTile(t, a, id)
	}
	if ua != "ntmaps-test/1.0" {
		t.Fatalf("User-Agent = %q", ua)
	}
	whole := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data) // ignores Range
	}))
	t.Cleanup(whole.Close)
	if _, err := OpenURL(whole.URL+"/x.pmtiles", nil, "", Options{}); err == nil {
		t.Fatal("a server without range support must be reported")
	}
}

func TestArchiveRejectsUnsupportedAndCorrupt(t *testing.T) {
	data := buildArchive(t, flatironTiles(), fixture{})
	bad := append([]byte(nil), data...)
	copy(bad, "NOTPMT!")
	if _, err := New(bytes.NewReader(bad), Options{}); err == nil {
		t.Fatal("bad magic accepted")
	}
	zstd := append([]byte(nil), data...)
	zstd[97] = CompressionZstd
	if _, err := New(bytes.NewReader(zstd), Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("zstd: %v", err)
	}
	mvt := append([]byte(nil), data...)
	mvt[99] = TypeMVT
	if _, err := New(bytes.NewReader(mvt), Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("mvt: %v", err)
	}
	if _, err := New(bytes.NewReader(data[:50]), Options{}); err == nil {
		t.Fatal("truncated archive accepted")
	}
	if _, err := DecodeDirectory([]byte{0xff, 0xff}); err == nil {
		t.Fatal("corrupt directory accepted")
	}
}

func TestDecodeDirectoryOffsets(t *testing.T) {
	entries := []Entry{{TileID: 5, Offset: 0, Length: 10, RunLength: 1}, {TileID: 6, Offset: 10, Length: 5, RunLength: 2}, {TileID: 20, Offset: 100, Length: 1, RunLength: 0}}
	got, err := DecodeDirectory(EncodeDirectory(entries))
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if got[i] != entries[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], entries[i])
		}
	}
	if e, ok := find(got, 7); !ok || e.TileID != 6 {
		t.Fatalf("find(7) = %+v %v, want the run starting at 6", e, ok)
	}
	if _, ok := find(got, 8); ok {
		t.Fatal("find(8) should miss: past the run and before the leaf")
	}
	if e, ok := find(got, 1000); !ok || e.RunLength != 0 {
		t.Fatal("find(1000) should land on the leaf pointer")
	}
	if _, ok := find(got, 2); ok {
		t.Fatal("find(2) should miss: before the first entry")
	}
}

func TestWriteProducesAReadableArchive(t *testing.T) {
	tiles := flatironTiles()
	// Two tiles with identical content share one blob.
	var first tile.ID
	for first = range tiles {
		break
	}
	dup := tile.ID{Z: first.Z, X: first.X + 10, Y: first.Y}
	tiles[dup] = tiles[first]
	var buf bytes.Buffer
	if err := Write(&buf, tiles, WriteOptions{Name: "fx", Attribution: "Written (c)", LeafSize: 5}); err != nil {
		t.Fatal(err)
	}
	a, err := New(bytes.NewReader(buf.Bytes()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if h := a.Header(); h.TileContents != uint64(len(tiles)-1) || h.TileEntries != uint64(len(tiles)) || h.MinZoom != 10 || h.MaxZoom != 12 || h.MinLon >= flatironLng || h.MaxLon <= flatironLng {
		t.Fatalf("header = %+v", h)
	}
	if a.Info().Attribution != "Written (c)" || len(a.root) >= len(tiles) {
		t.Fatalf("info = %+v root=%d", a.Info(), len(a.root))
	}
	for id := range tiles {
		if id == dup {
			continue
		}
		checkTile(t, a, id)
	}
	img, err := a.Tile(context.Background(), dup)
	if err != nil || color.RGBAModel.Convert(img.At(1, 1)).(color.RGBA) != tileColor(first) {
		t.Fatalf("deduplicated tile: %v", err)
	}
	if err := Write(&buf, nil, WriteOptions{}); err == nil {
		t.Fatal("empty archive accepted")
	}
	if err := Write(&buf, map[tile.ID][]byte{{Z: 2, X: 9, Y: 0}: {1}}, WriteOptions{}); err == nil {
		t.Fatal("out-of-range tile accepted")
	}
}
