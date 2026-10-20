# `ntmaps` CHANGELOG

## Unreleased

- Rename the project from `ntcharts-osm` to `ntmaps` and move it to the
  NimbleTerminal organization. The Go module path is now
  `nimbleterminal.dev/ntmaps` (previously `github.com/NimbleMarkets/ntcharts-osm`);
  the `mapview` package moves to the module root as package `ntmaps`, so
  `mapview.New` becomes `ntmaps.New` and so on; the API is otherwise
  unchanged. Example binaries are now named `ntmaps-*`, the explorer neighborhood cache moved to `ntmaps/neighborhoods-v1`
  under the user cache directory, and the browser demo lives at
  https://nimbleterminal.github.io/ntmaps/.

- Replace go-staticmaps with an in-house tile layer, `nimbleterminal.dev/ntmaps/tile`,
  and render progressively: a render paints the tiles it already has, fetches
  the rest, and repaints as they land; panning across cached tiles composes
  synchronously with no fetch and no Loading badge. Tiles are cached in memory
  and, natively, on disk under the user cache directory, and requests carry an
  identifying User-Agent. New `Config` fields `Loader`, `TileCacheCap`,
  `TileCacheDir`, `DisableDiskCache`, and `UserAgent`; new `SetTileSource`
  and `TileSource` for any tile source, with `TileStyle` reporting
  `StyleCustom`. The `go-staticmaps` replace directive is gone from both modules.
- Breaking: `StamenToner` and `StamenTerrain` (service shut down) and
  `OpenCycleMap` (http only, key-gated upstream) are removed from `Style`, so
  the remaining constants have new values; the oversample zoom cap follows the
  provider's `MaxZoom`; the attribution strip follows the selected provider;
  and the first frame of a render may show placeholder tiles while fetching.
- Replace the oversized pan buffer with a prefetch ring: after a render
  completes, the tiles one ring beyond the view (`Config.Prefetch`, default 1)
  are fetched in the background, and every pan, animated or not, composes
  from the tile cache. `Config.PanBuffer` is removed and `SmoothPan` no longer
  depends on it. Kitty back-pressure now applies to every presentation, and
  tile downloads share one concurrency limit per loader.
- Draw marker pins and the attribution strip without fogleman/gg: pins are
  rasterized from a signed distance with an anti-aliased outline and the
  credit uses x/image's basicfont, so gg and freetype leave the dependency
  list.
- Tiles come from a `tile.Source` interface (`Info` plus `Tile`), so offline
  files and generators are drop-ins; `tile.Provider` stays the XYZ-server
  implementation and now carries its own `Client` and `UserAgent`.
  `SetTileSource`/`TileSource` replace `SetTileProvider`/`TileProvider`, and
  the `Style` enum, `SetStyle`, and `TileStyle` are gone: pass
  `tile.OpenStreetMap()` and friends directly.
- Geocoding moves to `nimbleterminal.dev/ntmaps/geocode` behind a `Geocoder`
  interface (`Config.Geocoder`); the default Nominatim client sends the
  identifying User-Agent and paces requests as the usage policy requires. The
  `NominatimResponse` type is no longer exported.
- The attribution row under the map follows the current source instead of
  always crediting OpenStreetMap (`AttributionText` is removed), and the arrow
  keys zoom between 1 and the source's `MaxZoom` instead of 2 and 16.
- Mouse support in the widget: left drag pans, the wheel zooms around the
  cursor, events are read in the widget's cell coordinates, and hosts get
  `OffsetMouse` and `Dragging()` for routing. The explorer forwards its map
  events instead of carrying its own drag and zoom code.

## v0.3.0 (2026-10-19)

- Fix explorer mouse reporting and pane-relative 3D coordinates; add 2D
  drag/wheel navigation, sidebar selection, and capture cleanup on view changes.

- Split the minimal 2D map example from the explorer sample app; add 2D/3D
  switching, shared city rendering, bounded neighborhood caching, cancellation,
  and an explorer browser demo with lifecycle checks.

- Add experimental internal building-data/projection/extrusion helpers and a
  native city viewer with a bundled OSM neighborhood, height provenance, and
  camera movement; no public API is exposed yet.
- Add live software/GPU switching to the 3D demo with `t`, and show the active
  renderer and fallback errors without resetting the map or camera.
- Move map rendering from NTCharts picture to ntpicture v0.1.1's legacy API,
  retaining the existing map API and buffered-panning behavior.
- Upgrade the 3D map demo to ntcharts3d v0.3.1 (nt3d v0.2.0/ntpicture), share terminal
  discovery with the map source, and dispose chart placements before quitting.
- Isolate demos in an examples module; update booba to v0.10.1 and publish both
  demos with browser action controls, runtime validation, and WASM smoke tests.
- Opt into graceful action-metadata fallback so oversized descriptions cannot
  prevent demo startup; keyboard controls remain available.
- Use released texture fast paths to eliminate per-pixel texture allocations.
- deps: update ntcharts3d to v0.3.2, nt3d to v0.3.1, ntpicture to v0.1.1,
  go-booba to v0.10.3, the Bubble Tea WASM fork to `aa0ac3f1b5fe`,
  `actions/setup-node` to v7, and the remaining transitive dependencies to their
  latest compatible versions.

## v0.2.0 (2026-09-29)

 * feat(demo): add a textured ntcharts3d map surface with independent camera and map controls.
 * feat(mapview): add optional Harmonica v0.2.0 spring animation for buffered arrow panning; enable smooth movement in the native demo.
 * feat(mapview): add optional buffered panning with background border refills and serialized Kitty presentation; enable it in the native demo and use smaller screen-relative pan steps.
 * fix(mapview): preserve the glyph cache across value-receiver redraws and avoid re-rendering the map when switching display modes.
 * fix(mapview): safely wrap and cull offscreen markers when using existing go-staticmaps releases.
 * feat(demo): add `-medium direct|shm` for native Kitty transport, and `mapview.Model.Close()` to release pending shared-memory images on exit.
 * deps: upgrade ntcharts to v2.4.0 and go-booba to v0.7.0, which include Kitty shared-memory support.
 * feat(mapview): expose `Config.KittyMedium` and enable shared-memory transport for Kitty rendering in the WASM browser demo. Native demos and the default config retain direct transmission.
 * deps: raise the minimum Go version to 1.26.8 and update the Bubble Tea WASM fork to `1b36865b418a`, as required by go-booba v0.7.0.
 * deps: update Bubble Tea to v2.0.10, Bubbles to v2.2.1, Lip Gloss to v2.0.6, and the remaining application and tool dependencies to their latest compatible versions; tidy module checksums.

## v0.1.2 (2026-05-19)

 * feat(mapview): add `mapview.Model.Image()` to access the composited map `Image`.

## v0.1.1 (2026-05-14)

 * feat(mapview): add `IsMapOwnMsg` function for message routing

## v0.1.0 (2026-05-13)

 * Initial release
