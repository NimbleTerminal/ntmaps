# NimbleTerminal migration notes

This repository uses published ntpicture v0.1.1, ntcharts3d v0.3.2, nt3d v0.3.1,
and booba v0.10.3. No sibling checkout replacements are required.

## ntpicture Picture migration (ntmaps v0.5.0)

ntmaps now holds `*ntpicture.Picture`. No library package imports the legacy API.
The existing `RenderMode`, `FitMode`, and `KittyMedium` aliases remain compatible.
`Config.Session` accepts the host's terminal session; nil creates a map-owned
session. `Config.NextImageID` accepts the host's allocator and defaults to
`ntpicture.NextImageID`. PreferKitty follows that session's capability.

Picture prepares glyph and Kitty frames asynchronously. ntmaps waits for the
current presentation to settle before submitting its newest pending immutable
image. Tile render generations still reject stale results, independently of
Picture's presentation generations. Cache hits and optical zoom use the same
queue. Hosts must execute returned commands and forward `IsMapUpdate` messages
even when the map is unfocused. This includes private Picture completion and
session messages, not just exported PresentedMsg/ErrorMsg notifications.

Run `Dispose()` before removing the map or quitting, then `Close()`. For a
main-screen exit retaining the frame, include the map in the final main-screen
view and run `Keep()` before quitting, then `Close()`. A supplied terminal session
is never closed by the map. Close is idempotent; terminal cleanup requires running
the Dispose/Keep command while the event loop is alive.

The glyph cache regression remains covered by
`TestGlyphCacheSurvivesValueReceiverView`, now against Picture's committed view.

## ntcharts3d

The textured Surface/Material/Texture APIs continue to work. The mapview3d and
explorer examples create explicit terminal sessions and share them with map
sources through `Config.Session` and charts through `WithTerminalSession`.
Quit executes `Dispose()` before leaving the event loop; `Close()` runs afterward.
Native GPU support requires the `webgpu` build tag. Software rendering remains
the default and the browser demo uses it.

## Demo deployment

The examples module owns booba and its asset tool. Both demos run with
`booba.Run` through the shared action wrapper. Native tasks use `GOWORK=off`;
browser builds select the workspace containing both modules and the pinned
Bubble Tea/clipboard forks. Dependency replace directives must be repeated in
the examples module (Go does not inherit them from the library).

The action status protocol caps decoded snapshots at 2,048 bytes. Advertising
all 18 individual 3D controls exceeded that limit and prevented startup; the
strict wrapper validated the snapshot before booba started. With v0.10.1, our
wrapper opts into `NewModelWithFallback`: invalid action metadata emits a
diagnostic and disables action publication while keyboard controls and startup
continue. `NewModel` still has strict behavior; upgrading alone is insufficient.
The size limit remains unchanged. Keep the browser bar to a
compact subset and retain full keyboard instructions inside the terminal.
Include this limit and a startup smoke test in the deployment guide.

Export assets with `go -C examples tool booba-assets --no-index ../web/` so the
custom landing page survives. Runtime exports and generated files are checked;
Node runs each WASM binary through startup, action negotiation, resize, input,
and quit. These are lifecycle checks, not visual or live tile-fetch checks.

## Suggested platform guide corrections

The sibling ntpicture MIGRATION.md inspected during this migration still says
no siblings have migrated/released and describes ntcharts3d migration as future
work. Update that status for ntcharts3d v0.3.0 and ntqrcode's existing migration.
Add the Picture migration and host session/lifecycle pattern, plus a multi-module
consumer example covering workspace selection, replace inheritance, action
controls, and --no-index asset export.

Native Kitty, browser pixel rendering, live tile requests/CORS, and hardware GPU
behavior still need visual/manual acceptance; automated checks do not establish
those results. No sibling guide or dependency source was edited here.

## Offline defaults (ntmaps v0.5.0)

`New`, `NewWithConfig`, and `Render` no longer choose OpenStreetMap implicitly.
Pass `Config.Source`, call `SetTileSource`, or supply `RenderOptions.Source` for
base tiles. Nil draws a background and still supports markers and paths.
`Config.Geocoder` is nil by default; `SetLocation` returns an error message until
a geocoder is supplied. `Config.Prefetch` defaults to zero, with positive values
opting in. Keep prefetch off for OSMF tile servers and use an app-specific
User-Agent for online services. The native/browser examples opt in explicitly
and keep their existing tile caches. See README for the service policies.
