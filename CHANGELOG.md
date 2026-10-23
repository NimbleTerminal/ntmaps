# `ntmaps` CHANGELOG

## v0.5.0 (2026-10-23)

- Breaking: maps and headless `Render` now default to no tile source, drawing
  a placeholder without network access. Select `Config.Source`, `SetTileSource`,
  or `RenderOptions.Source` explicitly. The archive-download tool requires `-url`.
- Breaking: nil `Config.Geocoder` disables place lookup; `SetLocation` delivers
  a no-geocoder-configured error. Supply a geocoder explicitly.
- Breaking: `Config.Prefetch: 0` now disables look-ahead fetching; positive
  values opt in only for providers that permit it. Interactive examples retain
  explicit OSM/Nominatim configuration and an app-specific User-Agent.
- Enable mouse reporting in mapview: drag to pan and wheel to zoom.

- Move map presentation to `ntpicture.Picture`, with host-supplied terminal
  sessions and image ID allocation, `Dispose`/`Keep`/`Close` lifecycle methods,
  and asynchronous frame coalescing for glyph and Kitty rendering.

- `geodata` reads GeoJSON and GPX into markers and paths: points and
  waypoints as markers, lines, routes and track segments as paths, polygon
  rings as thinner outlines, with each feature's name and the box around
  them all. Reads are bounded by `Limits`. The mapview example shows a file
  with `-geodata`.

## v0.4.0 (2026-10-21)

The project is now `ntmaps` under NimbleTerminal, and the map is composed
from tiles in-house instead of through go-staticmaps.

- Breaking: the module is `nimbleterminal.dev/ntmaps` and the root package
  is `ntmaps`, so `mapview.New` becomes `ntmaps.New`; the API is otherwise
  the same. Example binaries are `ntmaps-*`, the explorer cache moves to
  `ntmaps/neighborhoods-v1`, and the browser demo lives at
  https://nimbleterminal.github.io/ntmaps/.
- Breaking: tiles come from a `tile.Source` (`Info` plus `Tile`), and
  `SetTileSource`/`TileSource` replace `SetTileProvider`/`TileProvider`.
  Pass `tile.OpenStreetMap()` and friends directly: the `Style` enum,
  `SetStyle`, `TileStyle`, `AttributionText`, `Config.PanBuffer`, and
  `NominatimResponse` are gone, `Config.CacheCap` is `RenderCacheCap`, and
  the Stamen and OpenCycleMap providers are removed.
- Breaking: the curated `Place` list moves to examples/internal/places, and
  `SetLocation` returns the lookup Cmd instead of waiting for the next message.
- Progressive rendering: a render paints the tiles it has, fetches the rest,
  and repaints as they land. Tiles are cached in memory and, natively, on
  disk, and a prefetch ring keeps pans composing from cache. New `Config`
  fields: `Loader`, `TileCacheCap`, `TileCacheDir`, `DisableDiskCache`,
  `UserAgent`, and `Prefetch`. go-staticmaps, gg, and freetype leave the
  dependency list.
- Mouse: left drag pans and the wheel zooms around the cursor, with
  `OffsetMouse` and `Dragging()` for hosts.
- Paths (`SetPaths`, `ClearPaths`), transparent overlays (`SetOverlays`,
  `Overlays`, with `tile.OpenSeaMap()` and `tile.OpenRailwayMap()` built in),
  and framing (`Bounds`, `SetBounds`, `ContentBounds`, `FitContent`).
- `Render` composes a static map image from `RenderOptions` with no terminal.
- Geocoding moves to `nimbleterminal.dev/ntmaps/geocode` behind a `Geocoder`
  interface (`Config.Geocoder`); the Nominatim client identifies itself and
  paces requests as the usage policy requires.
- Attribution follows the source, zoom is clamped to the source's `MaxZoom`,
  and requests carry a `User-Agent`; set `Config.UserAgent` before shipping.
- Offline: `tile/pmtiles` reads raster PMTiles v3 archives from a file or
  over HTTP range requests, `tile.FirstOf` layers sources offline-first, and
  `pmtiles.Write` with the `pmtiles-fetch` tool builds archives.
- Demos: the explorer cycles five styles (`s`), toggles seamarks (`o`), draws
  routes and frames them with `F`, geocodes with `G`, and serves Flatiron
  from a bundled archive; mapview takes `-pmtiles` and `-snapshot`. The demo
  page warns on an insecure origin, where Safari runs without a JIT or WebGPU.
- deps: go-booba v0.10.4 turns touch gestures into mouse input, so the
  explorer works on an iPad.

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
