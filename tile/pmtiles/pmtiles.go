// Package pmtiles reads raster tiles from a PMTiles v3 archive, a single
// file holding a whole tile set, from a local file or over HTTP range
// requests. An Archive is a tile.Source, so it is a drop-in for the
// online providers and makes ntmaps usable with no network at all.
//
// The format is specified at https://github.com/protomaps/PMTiles. This
// reader supports archives whose directories and tiles are stored
// uncompressed or gzip-compressed, which covers raster archives in
// practice; brotli and zstd archives are reported as unsupported.
package pmtiles

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"nimbleterminal.dev/ntmaps/internal/httpua"
	"nimbleterminal.dev/ntmaps/tile"
)

// HeaderSize is the fixed size of the archive header in bytes.
const HeaderSize = 127

var magic = []byte("PMTiles")

// Compression codes from the specification.
const (
	CompressionUnknown uint8 = 0
	CompressionNone    uint8 = 1
	CompressionGzip    uint8 = 2
	CompressionBrotli  uint8 = 3
	CompressionZstd    uint8 = 4
)

// Tile type codes from the specification.
const (
	TypeUnknown uint8 = 0
	TypeMVT     uint8 = 1
	TypePNG     uint8 = 2
	TypeJPEG    uint8 = 3
	TypeWebP    uint8 = 4
	TypeAVIF    uint8 = 5
)

// ErrUnsupported is returned for archives using a compression or tile
// type this reader cannot handle.
var ErrUnsupported = errors.New("pmtiles: unsupported archive")

// Header is the decoded archive header.
type Header struct {
	Version             uint8
	RootOffset          uint64
	RootLength          uint64
	MetadataOffset      uint64
	MetadataLength      uint64
	LeafOffset          uint64
	LeafLength          uint64
	TileDataOffset      uint64
	TileDataLength      uint64
	AddressedTiles      uint64
	TileEntries         uint64
	TileContents        uint64
	Clustered           bool
	InternalCompression uint8
	TileCompression     uint8
	TileType            uint8
	MinZoom, MaxZoom    uint8
	MinLon, MinLat      float64
	MaxLon, MaxLat      float64
	CenterZoom          uint8
	CenterLon           float64
	CenterLat           float64
}

// ParseHeader decodes the first HeaderSize bytes of an archive.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("pmtiles: header is %d bytes, want %d", len(b), HeaderSize)
	}
	if !bytes.Equal(b[:7], magic) {
		return Header{}, errors.New("pmtiles: bad magic, not a PMTiles archive")
	}
	u64 := func(off int) uint64 { return binary.LittleEndian.Uint64(b[off:]) }
	e7 := func(off int) float64 { return float64(int32(binary.LittleEndian.Uint32(b[off:]))) / 1e7 }
	h := Header{
		Version:             b[7],
		RootOffset:          u64(8),
		RootLength:          u64(16),
		MetadataOffset:      u64(24),
		MetadataLength:      u64(32),
		LeafOffset:          u64(40),
		LeafLength:          u64(48),
		TileDataOffset:      u64(56),
		TileDataLength:      u64(64),
		AddressedTiles:      u64(72),
		TileEntries:         u64(80),
		TileContents:        u64(88),
		Clustered:           b[96] == 1,
		InternalCompression: b[97],
		TileCompression:     b[98],
		TileType:            b[99],
		MinZoom:             b[100],
		MaxZoom:             b[101],
		MinLon:              e7(102),
		MinLat:              e7(106),
		MaxLon:              e7(110),
		MaxLat:              e7(114),
		CenterZoom:          b[118],
		CenterLon:           e7(119),
		CenterLat:           e7(123),
	}
	if h.Version != 3 {
		return h, fmt.Errorf("%w: version %d", ErrUnsupported, h.Version)
	}
	return h, nil
}

// Entry is one directory entry: a run of tiles sharing one blob, or,
// when RunLength is zero, a pointer to a leaf directory.
type Entry struct {
	TileID    uint64
	Offset    uint64
	Length    uint32
	RunLength uint32
}

// Options tunes an Archive.
type Options struct {
	// Name keys ntmaps' caches. Empty means the file's base name or the
	// URL's last path element.
	Name string

	// TileSize is the pixel size of the archive's tiles. Zero means 256.
	TileSize int

	// Attribution overrides the archive metadata's attribution.
	Attribution string

	// Placeholder fills missing tiles; see tile.Info.
	Placeholder color.Color

	// MaxDirectoryBytes bounds a directory or the metadata, stored and
	// decompressed, so an archive cannot claim more memory than this for
	// one read. Zero means DefaultMaxDirectoryBytes.
	MaxDirectoryBytes int

	// MaxTileBytes bounds one tile, stored and decompressed. Zero means
	// DefaultMaxTileBytes.
	MaxTileBytes int
}

// The default bounds on an archive's reads: an archive is often a file
// someone else made, and its header and directories say how much to read.
const (
	DefaultMaxDirectoryBytes = 16 << 20
	DefaultMaxTileBytes      = 8 << 20
)

// maxLeaves is how many decoded leaf directories an Archive keeps; past it
// the cache starts over.
const maxLeaves = 64

// ErrTooLarge is a directory, the metadata or a tile larger than its bound
// in Options.
var ErrTooLarge = errors.New("pmtiles: larger than the archive's read limit")

// Archive reads tiles from a PMTiles file. It is safe for concurrent use.
type Archive struct {
	r      io.ReaderAt
	closer io.Closer
	header Header
	info   tile.Info
	root   []Entry

	maxDirectory, maxTile int

	mu     sync.Mutex
	leaves map[uint64][]Entry // decoded leaf directories by offset
}

// New reads the header, root directory, and metadata from r. Pass the
// zero Options for defaults.
func New(r io.ReaderAt, o Options) (*Archive, error) {
	hb := make([]byte, HeaderSize)
	if _, err := r.ReadAt(hb, 0); err != nil {
		return nil, fmt.Errorf("pmtiles: read header: %w", err)
	}
	h, err := ParseHeader(hb)
	if err != nil {
		return nil, err
	}
	switch h.TileType {
	case TypePNG, TypeJPEG, TypeUnknown:
	default:
		return nil, fmt.Errorf("%w: tile type %d is not a raster this reader decodes", ErrUnsupported, h.TileType)
	}
	if !supportedCompression(h.InternalCompression) || !supportedCompression(h.TileCompression) {
		return nil, fmt.Errorf("%w: compression %d/%d", ErrUnsupported, h.InternalCompression, h.TileCompression)
	}
	a := &Archive{r: r, header: h, leaves: make(map[uint64][]Entry),
		maxDirectory: o.MaxDirectoryBytes, maxTile: o.MaxTileBytes}
	if a.maxDirectory <= 0 {
		a.maxDirectory = DefaultMaxDirectoryBytes
	}
	if a.maxTile <= 0 {
		a.maxTile = DefaultMaxTileBytes
	}
	root, err := a.readDirectory(h.RootOffset, h.RootLength)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: root directory: %w", err)
	}
	a.root = root

	attribution := o.Attribution
	if attribution == "" {
		attribution = a.metadataAttribution()
	}
	a.info = tile.Info{
		Name:        o.Name,
		Attribution: attribution,
		TileSize:    o.TileSize,
		MaxZoom:     int(h.MaxZoom),
		Placeholder: o.Placeholder,
	}
	if a.info.Name == "" {
		a.info.Name = "pmtiles"
	}
	return a, nil
}

func supportedCompression(c uint8) bool {
	return c == CompressionNone || c == CompressionGzip || c == CompressionUnknown
}

// Open opens a local archive. Close releases the file.
func Open(path string, o Options) (*Archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if o.Name == "" {
		o.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	a, err := New(f, o)
	if err != nil {
		f.Close()
		return nil, err
	}
	a.closer = f
	return a, nil
}

// OpenURL opens an archive served over HTTP with range requests. The
// client may be nil; the User-Agent follows ntmaps' convention and is
// not sent under GOOS=js.
func OpenURL(url string, client *http.Client, userAgent string, o Options) (*Archive, error) {
	if o.Name == "" {
		base := url[strings.LastIndex(url, "/")+1:]
		o.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return New(&httpReaderAt{url: url, client: client, ua: userAgent}, o)
}

// Close releases the underlying file, if any.
func (a *Archive) Close() error {
	if a.closer != nil {
		return a.closer.Close()
	}
	return nil
}

// Header returns the decoded archive header.
func (a *Archive) Header() Header { return a.header }

// Bounds returns the archive's declared extent.
func (a *Archive) Bounds() (south, west, north, east float64) {
	h := a.header
	return h.MinLat, h.MinLon, h.MaxLat, h.MaxLon
}

// Info implements tile.Source.
func (a *Archive) Info() tile.Info { return a.info }

// Tile implements tile.Source.
func (a *Archive) Tile(ctx context.Context, id tile.ID) (image.Image, error) {
	data, err := a.TileData(ctx, id)
	if err != nil {
		return nil, err
	}
	img, err := tile.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: decode %d/%d/%d: %w", id.Z, id.X, id.Y, err)
	}
	return img, nil
}

// TileData implements tile.DataSource, returning the stored bytes after
// undoing the archive's tile compression.
func (a *Archive) TileData(ctx context.Context, id tile.ID) ([]byte, error) {
	if id.Z < 0 || id.Z > 31 || id.Z < int(a.header.MinZoom) || id.Z > int(a.header.MaxZoom) {
		return nil, tile.ErrNotFound
	}
	n := 1 << uint(id.Z)
	if id.X < 0 || id.Y < 0 || id.X >= n || id.Y >= n {
		return nil, tile.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	want := ZxyToID(uint8(id.Z), uint32(id.X), uint32(id.Y))
	entries := a.root
	for depth := 0; depth < 4; depth++ {
		e, ok := find(entries, want)
		if !ok {
			return nil, tile.ErrNotFound
		}
		if e.RunLength > 0 {
			data, err := a.read(a.header.TileDataOffset, e.Offset, uint64(e.Length), a.maxTile)
			if err != nil {
				return nil, fmt.Errorf("pmtiles: read tile: %w", err)
			}
			return decompress(data, a.header.TileCompression, a.maxTile)
		}
		leaf, err := a.leaf(e)
		if err != nil {
			return nil, err
		}
		entries = leaf
	}
	return nil, errors.New("pmtiles: directory nesting too deep")
}

// find returns the entry covering id: the entry with the greatest TileID
// not above id, when id falls within its run or it is a leaf pointer.
func find(entries []Entry, id uint64) (Entry, bool) {
	i := sort.Search(len(entries), func(i int) bool { return entries[i].TileID > id }) - 1
	if i < 0 {
		return Entry{}, false
	}
	e := entries[i]
	if e.RunLength == 0 || id < e.TileID+uint64(e.RunLength) {
		return e, true
	}
	return Entry{}, false
}

func (a *Archive) leaf(e Entry) ([]Entry, error) {
	off := a.header.LeafOffset + e.Offset
	a.mu.Lock()
	entries, ok := a.leaves[off]
	a.mu.Unlock()
	if ok {
		return entries, nil
	}
	if off < a.header.LeafOffset {
		return nil, fmt.Errorf("pmtiles: leaf offset %d is out of range", e.Offset)
	}
	entries, err := a.readDirectory(off, uint64(e.Length))
	if err != nil {
		return nil, fmt.Errorf("pmtiles: leaf directory: %w", err)
	}
	a.mu.Lock()
	if len(a.leaves) >= maxLeaves {
		clear(a.leaves)
	}
	a.leaves[off] = entries
	a.mu.Unlock()
	return entries, nil
}

// read reads length bytes at base+off, refusing a length over limit or a
// position past what an offset can address, before allocating anything.
func (a *Archive) read(base, off, length uint64, limit int) ([]byte, error) {
	if length > uint64(limit) {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, length, limit)
	}
	at := base + off
	if at < base || at > math.MaxInt64-length {
		return nil, fmt.Errorf("pmtiles: offset %d+%d is out of range", base, off)
	}
	data := make([]byte, length)
	if _, err := a.r.ReadAt(data, int64(at)); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *Archive) readDirectory(off, length uint64) ([]Entry, error) {
	raw, err := a.read(0, off, length, a.maxDirectory)
	if err != nil {
		return nil, err
	}
	data, err := decompress(raw, a.header.InternalCompression, a.maxDirectory)
	if err != nil {
		return nil, err
	}
	return DecodeDirectory(data)
}

// DecodeDirectory decodes an uncompressed directory.
func DecodeDirectory(data []byte) ([]Entry, error) {
	rd := bytes.NewReader(data)
	n, err := binary.ReadUvarint(rd)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: directory count: %w", err)
	}
	if n > uint64(len(data))/4 { // Each entry takes at least four bytes.
		return nil, fmt.Errorf("pmtiles: directory claims %d entries in %d bytes", n, len(data))
	}
	entries := make([]Entry, n)
	var last uint64
	for i := range entries {
		d, err := binary.ReadUvarint(rd)
		if err != nil {
			return nil, fmt.Errorf("pmtiles: tile id %d: %w", i, err)
		}
		last += d
		entries[i].TileID = last
	}
	for i := range entries {
		v, err := binary.ReadUvarint(rd)
		if err != nil {
			return nil, fmt.Errorf("pmtiles: run length %d: %w", i, err)
		}
		entries[i].RunLength = uint32(v)
	}
	for i := range entries {
		v, err := binary.ReadUvarint(rd)
		if err != nil {
			return nil, fmt.Errorf("pmtiles: length %d: %w", i, err)
		}
		entries[i].Length = uint32(v)
	}
	for i := range entries {
		v, err := binary.ReadUvarint(rd)
		if err != nil {
			return nil, fmt.Errorf("pmtiles: offset %d: %w", i, err)
		}
		if v == 0 && i > 0 {
			entries[i].Offset = entries[i-1].Offset + uint64(entries[i-1].Length)
		} else {
			entries[i].Offset = v - 1
		}
	}
	return entries, nil
}

// decompress undoes compression, refusing output over limit bytes.
func decompress(data []byte, compression uint8, limit int) ([]byte, error) {
	switch compression {
	case CompressionNone, CompressionUnknown:
		return data, nil
	case CompressionGzip:
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("pmtiles: gzip: %w", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
		if err != nil {
			return nil, fmt.Errorf("pmtiles: gzip: %w", err)
		}
		if len(out) > limit {
			return nil, fmt.Errorf("%w: decompresses past %d bytes", ErrTooLarge, limit)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: compression %d", ErrUnsupported, compression)
	}
}

// Metadata returns the archive's JSON metadata document.
func (a *Archive) Metadata() ([]byte, error) {
	if a.header.MetadataLength == 0 {
		return []byte("{}"), nil
	}
	raw, err := a.read(0, a.header.MetadataOffset, a.header.MetadataLength, a.maxDirectory)
	if err != nil {
		return nil, fmt.Errorf("pmtiles: read metadata: %w", err)
	}
	return decompress(raw, a.header.InternalCompression, a.maxDirectory)
}

func (a *Archive) metadataAttribution() string {
	data, err := a.Metadata()
	if err != nil {
		return ""
	}
	var meta struct {
		Attribution string `json:"attribution"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.Attribution
}

// httpReaderAt serves ReadAt through HTTP range requests.
type httpReaderAt struct {
	url    string
	client *http.Client
	ua     string
}

var defaultClient = &http.Client{}

func (h *httpReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	req, err := http.NewRequest(http.MethodGet, h.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1))
	httpua.Set(req, h.ua)
	client := h.client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("pmtiles: range request to %s: %s", h.url, resp.Status)
	}
	n, err := io.ReadFull(resp.Body, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, io.EOF
	}
	return n, err
}
