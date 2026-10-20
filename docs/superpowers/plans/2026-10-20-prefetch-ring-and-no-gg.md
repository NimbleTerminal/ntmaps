# Prefetch Ring and gg Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the oversized pan buffer with a background prefetch ring so smooth panning composes from the tile cache, then draw markers and attribution without fogleman/gg.

**Architecture:** After a render completes, a prefetch Cmd asks the loader for the ring of tiles one or more tile widths beyond the view; the next pan finds them in memory and the synchronous compose path paints at once. The pan buffer, its refill scheduler, and `renderBuffer` go away; `panState` keeps only the spring motion and the Kitty presentation back-pressure, which every presentation now goes through. Markers become a signed-distance pin rasterizer and attribution uses x/image's basicfont, so gg and freetype leave go.mod.

**Tech Stack:** Go 1.26, Bubble Tea v2, ntpicture legacy model, golang.org/x/image/font/basicfont, net/http.

**Spec:** conversation of 2026-10-20 after the progressive-tiles commit.

## Global Constraints

- `Config.PanBuffer` is removed; `Config.Prefetch` is the ring width in tiles (0 → 1, negative → none, max 4). `Config.SmoothPan` no longer needs any other field.
- The loader bounds concurrent downloads globally (Options.Concurrency) so a prefetch never starves a render; a superseding render cancels the prefetch but shared downloads survive through the waiter count.
- No tile is cached in the render LRU while the spring animation is active.
- The pin keeps go-staticmaps' geometry: head radius size/2 centered size above the tip, 1px black outline, default red and 16px.
- Attribution text is basicfont Face7x13 on a half-transparent black strip, as gg drew it.
- `github.com/fogleman/gg` and `github.com/golang/freetype` leave go.mod; `golang.org/x/image` becomes direct.

## Review Focus

- Holding an arrow across unseen territory: frames keep repainting placeholder plus arriving tiles without a debounce stall. (Task 2 test: motion active skips the debounce.)
- A Kitty encode in progress: the latest image is queued and presented after the frame, never dropped. (Task 2 port of the rapid-input test.)
- A pan while a prefetch is running: the prefetch context is canceled, the new render still gets the tiles it shares. (Task 2 cancel test.)
- A marker near the image edge: the outline is clipped, not wrapped, and no out-of-bounds write happens. (Task 3 edge test.)
- Attribution longer than the image: text is clipped at the right edge. (Task 3.)

---

### Task 1: global download limit in the loader

**Files:** `tile/loader.go`, `tile/loader_test.go`.

Add `sem chan struct{}` sized Concurrency to Loader; `run` acquires it (select with ctx) around `load`. FetchAll drops its per-call semaphore. Test: a Loader with Concurrency 2 and a holding server sees at most 2 started requests for 5 concurrent Fetch calls.

### Task 2: prefetch ring replaces the pan buffer

**Files:** `ntmaps.go`, `pan.go`, `animation.go`, `job.go`, `render.go`, `pan_test.go`, `animation_test.go`, `progressive_test.go`, examples, README, CHANGELOG.

**Interfaces:**
```go
type Config struct{ …; Prefetch int }           // PanBuffer removed
func (m *Model) prefetchCmd() tea.Cmd            // nil when nothing to fetch
func (m *Model) presentImage(img image.Image) tea.Cmd
type panState struct{ motion panMotion; encoding bool; pending image.Image }
```
renderMapCmd: no buffered branch; synchronous composes present through presentImage, skip the render LRU during motion, and batch prefetchCmd. Debounce is skipped while motion is active. Update: final mapImageMsg presents, caches (not during motion), and batches prefetchCmd; panPresentedMsg presents pending. jobSlot gains the prefetch cancel. Delete panBuffer, bufferImageMsg, panRenderTickMsg, renderBufferedMapCmd, renderBuffer.
Tests: prefetch ring makes a one-tile pan synchronous with zero new requests; prefetch is canceled by a new render; rapid input under Kitty queues the latest frame; spring tests run against an empty provider without a seeded buffer.

### Task 3: drop gg

**Files:** `render.go`, `render_test.go`, go.mod, go.sum, examples/go.sum, THIRD_PARTY_NOTICES.md.

drawMarkers rasterizes the pin from a signed distance (circle ∪ triangle) with anti-aliased 1px outline, writing only inside img.Bounds(). drawMapAttribution uses basicfont.Face7x13 with font.Drawer and draw.Over for the strip. Tests: outline pixel at the head's edge is dark; interior is the fill color; a pin at the corner writes nothing outside bounds; attribution strip darkens the bottom rows and clips long text.
