# Progressive Tile Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ntmaps paints the tiles it already has, fetches the missing ones, repaints as they arrive, and no longer depends on go-staticmaps.

**Architecture:** A new `tile` subpackage owns providers (URL template, attribution, tile size, max zoom), Web Mercator projection and tile coverage, an in-memory LRU plus optional disk cache, and a de-duplicating fetcher with context cancellation. The root package composes tiles into the cell rectangle itself, draws markers and the attribution strip with gg, and turns a cache-miss render into a subscription: the first `mapImageMsg` carries the partially filled map and a `next` Cmd that waits for the next repaint until `done`.

**Tech Stack:** Go 1.26, Bubble Tea v2, ntpicture legacy model, fogleman/gg (markers and attribution text only), net/http, image/png, image/jpeg.

**Spec:** the conversation of 2026-10-20 (progressive rendering chosen as the feature that forces the go-staticmaps replacement).

## Global Constraints

- Module path `nimbleterminal.dev/ntmaps`; package `ntmaps` at the root; new package `nimbleterminal.dev/ntmaps/tile`.
- Drop `github.com/flopp/go-staticmaps` and `github.com/golang/geo` from go.mod; drop the go-staticmaps replace directive from both go.mod files.
- Tile requests send `User-Agent: ntmaps/<version> (+https://github.com/NimbleTerminal/ntmaps)` on native, and no User-Agent header under GOOS=js.
- Drop `StamenToner` and `StamenTerrain` from `Style` (service is gone). Remaining built-ins use https URLs.
- Image geometry stays identical to go-staticmaps: output pixel (w/2, h/2) is world pixel (floor(cx), floor(cy)); tile x wraps modulo 2^z; tile y outside the world is placeholder.
- Existing tests keep passing except where they construct go-staticmaps types; those are rewritten against the new seam.
- Tests never touch the network or the user's cache directory.

## Review Focus

- A render superseded while tiles are in flight: the old job stops waiting, but a tile two jobs both want is downloaded once and kept. (Task 3 loader de-dup test; Task 5 supersede test.)
- A provider with an empty URL (`tile.Provider{}`): compose placeholder and report done with no fetches, no error. (Task 5.)
- All tiles fail (offline): `mapImageMsg.err` set, View shows the error as today. One tile 404 among many: placeholder, no error. (Task 5.)
- View at the antimeridian or near the poles: x wraps, y rows outside the world are placeholder, no panic. (Task 2.)
- Marker whose pin straddles the left edge after wrap is still drawn. (Task 4, port of the safeMarker test.)

---

### Task 1: tile.Provider and built-ins

**Files:** Create `tile/provider.go`, `tile/provider_test.go`.

**Produces:**
```go
type Provider struct {
    Name, URL, Attribution string // URL uses {z} {x} {y} {s}
    Shards []string
    TileSize, MaxZoom int        // 0 → 256, 0 → 19
    Placeholder color.Color      // nil → light gray
}
func (p Provider) Valid() bool
func (p Provider) TileURL(z, x, y int) string
func (p Provider) Size() int
func (p Provider) Max() int
func OpenStreetMap(), Wikimedia(), OpenTopoMap(), OpenCycleMap(), CartoLight(), CartoDark(), ArcGISWorldImagery() Provider
```
Tests: TileURL substitutes and picks shard `(x+y)%len`; no shards leaves `{s}` absent; ArcGIS URL orders y before x; `Provider{}.Valid()` is false; defaults 256 and 19.

### Task 2: projection and coverage

**Files:** Create `tile/project.go`, `tile/project_test.go`.

**Produces:**
```go
type ID struct{ Z, X, Y int }
type Placement struct{ ID ID; Offset image.Point; InWorld bool }
func Project(lat, lng float64, zoom, tileSize int) (x, y, world float64)
func Unproject(x, y float64, zoom, tileSize int) (lat, lng float64)
func Cover(cx, cy float64, w, h, zoom, tileSize int) []Placement
```
Cover: origin world pixel = (floor(cx)-w/2, floor(cy)-h/2); tile range from floor(origin/size) to floor((origin+dim-1)/size); X normalized modulo 2^z; InWorld false when Y outside [0, 2^z). Tests: round trip Project/Unproject; a 512×512 view at z=1 centered on (0,0) yields 4 tiles with offsets (0,0),(256,0),(0,256),(256,256); view across lng 180 yields X wrapped to 0; view at lat 85 yields InWorld false rows.

### Task 3: loader with memory LRU, disk cache, de-dup, cancellation

**Files:** Create `tile/loader.go`, `tile/loader_test.go`, `tile/useragent_js.go`, `tile/useragent_other.go`, `tile/diskdir_js.go`, `tile/diskdir_other.go`.

**Produces:**
```go
type Loader struct{ /* unexported */ }
type Options struct{ Client *http.Client; UserAgent string; MemoryCap int; Dir string; Concurrency int }
func NewLoader(o Options) *Loader
func DefaultUserAgent() string
func DefaultDir() string  // UserCacheDir/ntmaps/tiles on native, "" on js
func (l *Loader) Cached(p Provider, id ID) (image.Image, bool)
func (l *Loader) Fetch(ctx context.Context, p Provider, id ID) (image.Image, error)
func (l *Loader) FetchAll(ctx context.Context, p Provider, ids []ID, onTile func(ID, image.Image)) error
```
Fetch: memory → disk → HTTP; stores decoded image in memory and raw bytes on disk; joins an in-flight download of the same tile; the download's own context is canceled only when every waiter has left. 404 returns `ErrNotFound`. FetchAll runs `Concurrency` workers (default 6) and returns the first error only if no tile succeeded.
Tests with httptest: second Fetch hits memory (server count 1); disk round trip in t.TempDir() with a fresh Loader; two concurrent Fetch for one tile → one request; canceling one of two waiters keeps the download alive and the other gets the image; User-Agent header matches; 404 → ErrNotFound; FetchAll with one 404 among three returns nil and calls onTile twice.

### Task 4: compose, markers, attribution in the root package

**Files:** Create `render.go`, `render_test.go`; modify `pan.go` (delete safeMarker/addMapMarkers; keep drawMapAttribution; worldPixels delegates to tile.Project).

**Produces:**
```go
func composeTiles(dst *image.RGBA, pl []tile.Placement, p tile.Provider, lookup func(tile.ID) (image.Image, bool)) (missing []tile.ID)
func drawMarkers(img *image.RGBA, markers []Marker, cx, cy, world float64, w, h int)
```
drawMarkers reproduces the go-staticmaps pin (arc + tail, 1px black stroke) at the pixel of each marker relative to the center using nearest world copy, skipping |lat| > 85 and pins fully outside. Tests: missing list equals the IDs lookup refused; placeholder color fills missing and out-of-world; pin straddling the left edge is drawn (port of TestSafeMarkerPreservesPinOverlappingViewport).

### Task 5: progressive render path

**Files:** Modify `ntmaps.go` (Model fields, Config, SetStyle, SetTileProvider, TileProvider, renderMapCmd, Update), modify `pan.go` (renderBufferedMapCmd), delete `tilecache_js.go`, `tilecache_other.go`; modify `ntmaps_test.go`, `pan_test.go`, `animation_test.go`.

**Interfaces:**
```go
type Config struct{ …; TileCacheCap int; TileCacheDir string; DisableDiskCache bool; UserAgent string }
func (m *Model) SetTileProvider(p tile.Provider) tea.Cmd
func (m Model) TileProvider() tile.Provider
const StyleCustom Style = -1
type mapImageMsg struct{ gen uint64; key renderKey; img image.Image; err error; done bool; next tea.Cmd }
```
renderMapCmd: render-LRU hit → as today. All tiles cached in the loader → compose synchronously, treat as hit. Otherwise start a renderJob (cancel the previous one), optionally debounced; its Cmd returns the first partial image with `next`. Update applies any matching-gen image; on `!done` returns `next`; on `done` sets lastAccepted and fills the render LRU. renderKey.style becomes the provider name. effectiveOversample caps at provider Max().
Buffered path: FetchAll then compose the padded buffer.
Tests: with an httptest provider, run the fetch Cmd and the chain of `next` Cmds, assert first message is partial with placeholder, last is done with tile pixels, inFlight true between; empty provider is done at once; a model whose gen moved on drops the message and the job's context is canceled; all-fail yields err; existing tests updated to `tile.Provider{}`.

### Task 6: dependency removal, docs, changelog

**Files:** go.mod, go.sum, examples/go.mod, examples/go.sum, wasm.work.sum, THIRD_PARTY_NOTICES.md, README.md, CHANGELOG.md.

Drop go-staticmaps and geo; remove the replace from both modules; `task go-tidy`, `task notices`; README: provider list (seven), custom provider example, progressive rendering note, User-Agent note; CHANGELOG Unreleased entry listing breaking changes (Stamen gone, renderKey, first frame may be partial).
Verify: `task ci` passes natively, `task build-wasm` and `task check-site` pass.
