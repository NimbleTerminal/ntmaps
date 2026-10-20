package pmtiles

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"nimbleterminal.dev/ntmaps/tile"
)

// WriteOptions describes the archive Write produces.
type WriteOptions struct {
	// Name and Attribution go into the metadata document; Attribution is
	// what an Archive reports as its credit.
	Name, Attribution string

	// TileType is TypePNG or TypeJPEG. Zero means PNG.
	TileType uint8

	// Bounds and center are advisory; zero values are derived from the
	// tiles.
	MinLon, MinLat, MaxLon, MaxLat float64
	CenterLon, CenterLat           float64
	CenterZoom                     uint8

	// LeafSize is the number of entries per leaf directory once the root
	// would exceed it. Zero means 4096; negative keeps a single root.
	LeafSize int
}

// Write encodes tiles as a clustered PMTiles v3 archive with gzip
// directories and uncompressed tile data, de-duplicating identical
// tiles. Keys are tile coordinates; values are encoded PNG or JPEG.
func Write(w io.Writer, tiles map[tile.ID][]byte, o WriteOptions) error {
	if len(tiles) == 0 {
		return errors.New("pmtiles: no tiles to write")
	}
	if o.TileType == 0 {
		o.TileType = TypePNG
	}
	if o.LeafSize == 0 {
		o.LeafSize = 4096
	}
	type keyed struct {
		id   uint64
		data []byte
	}
	all := make([]keyed, 0, len(tiles))
	minZ, maxZ := 255, 0
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for id, data := range tiles {
		n := 1 << uint(id.Z)
		if id.Z < 0 || id.Z > 31 || id.X < 0 || id.Y < 0 || id.X >= n || id.Y >= n {
			return fmt.Errorf("pmtiles: tile %v is out of range", id)
		}
		all = append(all, keyed{ZxyToID(uint8(id.Z), uint32(id.X), uint32(id.Y)), data})
		minZ, maxZ = min(minZ, id.Z), max(maxZ, id.Z)
		lat1, lng1 := tile.Unproject(float64(id.X*256), float64(id.Y*256), id.Z, 256)
		lat2, lng2 := tile.Unproject(float64((id.X+1)*256), float64((id.Y+1)*256), id.Z, 256)
		minX, maxX = min(minX, lng1), max(maxX, lng2)
		minY, maxY = min(minY, lat2), max(maxY, lat1)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })

	var tileData []byte
	var entries []Entry
	offsets := map[[32]byte]Entry{}
	contents := 0
	for _, k := range all {
		sum := sha256.Sum256(k.data)
		e, ok := offsets[sum]
		if !ok {
			e = Entry{Offset: uint64(len(tileData)), Length: uint32(len(k.data))}
			tileData = append(tileData, k.data...)
			offsets[sum] = e
			contents++
		}
		e.TileID, e.RunLength = k.id, 1
		entries = append(entries, e)
	}

	var root []Entry
	var leaves []byte
	if o.LeafSize > 0 && len(entries) > o.LeafSize {
		for i := 0; i < len(entries); i += o.LeafSize {
			chunk := entries[i:min(i+o.LeafSize, len(entries))]
			leaf := gzipBytes(EncodeDirectory(chunk))
			root = append(root, Entry{TileID: chunk[0].TileID, Offset: uint64(len(leaves)), Length: uint32(len(leaf))})
			leaves = append(leaves, leaf...)
		}
	} else {
		root = entries
	}
	rootBytes := gzipBytes(EncodeDirectory(root))
	metaJSON, _ := json.Marshal(map[string]any{"name": o.Name, "attribution": o.Attribution, "format": map[uint8]string{TypePNG: "png", TypeJPEG: "jpg"}[o.TileType]})
	meta := gzipBytes(metaJSON)

	if o.MinLon == 0 && o.MaxLon == 0 && o.MinLat == 0 && o.MaxLat == 0 {
		o.MinLon, o.MinLat, o.MaxLon, o.MaxLat = minX, minY, maxX, maxY
	}
	if o.CenterLon == 0 && o.CenterLat == 0 {
		o.CenterLon, o.CenterLat = (o.MinLon+o.MaxLon)/2, (o.MinLat+o.MaxLat)/2
	}
	if o.CenterZoom == 0 {
		o.CenterZoom = uint8(maxZ)
	}

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
	put(88, uint64(contents))
	h[96] = 1
	h[97] = CompressionGzip
	h[98] = CompressionNone
	h[99] = o.TileType
	h[100] = uint8(minZ)
	h[101] = uint8(maxZ)
	e7(102, o.MinLon)
	e7(106, o.MinLat)
	e7(110, o.MaxLon)
	e7(114, o.MaxLat)
	h[118] = o.CenterZoom
	e7(119, o.CenterLon)
	e7(123, o.CenterLat)

	for _, part := range [][]byte{h, rootBytes, meta, leaves, tileData} {
		if _, err := w.Write(part); err != nil {
			return err
		}
	}
	return nil
}

// EncodeDirectory encodes entries, which must be sorted by TileID, in
// the uncompressed directory format.
func EncodeDirectory(entries []Entry) []byte {
	var buf []byte
	buf = binary.AppendUvarint(buf, uint64(len(entries)))
	var last uint64
	for _, e := range entries {
		buf = binary.AppendUvarint(buf, e.TileID-last)
		last = e.TileID
	}
	for _, e := range entries {
		buf = binary.AppendUvarint(buf, uint64(e.RunLength))
	}
	for _, e := range entries {
		buf = binary.AppendUvarint(buf, uint64(e.Length))
	}
	for i, e := range entries {
		if i > 0 && e.Offset == entries[i-1].Offset+uint64(entries[i-1].Length) {
			buf = binary.AppendUvarint(buf, 0)
		} else {
			buf = binary.AppendUvarint(buf, e.Offset+1)
		}
	}
	return buf
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}
