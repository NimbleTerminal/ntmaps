# NimbleTerminal migration notes

This repository uses published ntpicture v0.1.0, ntcharts3d v0.3.1, nt3d v0.2.0,
and booba v0.9.2. No sibling checkout replacements are required.

## ntpicture compatibility is a two-import migration

The old NTCharts picture package does not map to a single replacement import:

| Old symbol | Replacement |
| --- | --- |
| `picture.New()` | `legacy.NewModel()` |
| `picture.Model`, `KittyFrameMsg`, `IsPictureMsg` | `nimbleterminal.dev/ntpicture/legacy` |
| Process-global capability helpers | `nimbleterminal.dev/ntpicture/legacy` |
| `PictureMode`, `FitMode`, `KittyMedium`, capability enums | `nimbleterminal.dev/ntpicture` |

mapview retains the legacy frame API because its buffered-pan generation and
presentation coordination already depend on it. This is a compatibility
migration, not conversion to the root `Picture` asynchronous presentation API.
Existing public mapview aliases remain available. Hosts must execute commands
returned by Update, including terminal capability notifications.

## Confirmed upstream bug: legacy View loses its glyph cache

In ntpicture v0.1.0, `legacybridge.ModelView` type-asserts the stored value to
`legacyModel`, calls View on that copy, and returns only tea.View. View fills
`glyphKey` and `glyphCache`, but the bridge discards the changed model.
`ModelString` has the same copy behavior.

The existing `TestGlyphCacheSurvivesValueReceiverView` failed after migration:
a second unchanged View read source pixels again. `mapview/picture.go` caches
the presentation and invalidates it on image, size, fit, mode, and picture
message changes. This narrow adapter preserves mapview performance until an
upstream release fixes the bridge. Remove it only when that regression test
passes without the adapter.

Suggested upstream fix: retain updated state across ModelView/ModelString
(either return and store it, or consistently use pointer-backed bridge state).
Add a source-pixel-read regression test against the public legacy API; testing
only the internal model misses the bridge bug.

## ntcharts3d v0.3.0

The textured Surface/Material/Texture APIs continue to work. The example now
imports ntpicture types directly and shares `legacy.DefaultTerminalSession()`
with its map source through `WithTerminalSession`, avoiding separate discovery
state. Quit executes `Dispose()` before leaving the event loop; `Close()` still
runs afterward. Native GPU support requires the `webgpu` build tag. Software
rendering remains the default and the browser demo uses it.

## Demo deployment

The examples module owns booba and its asset tool. Both demos run with
`booba.Run` through the shared action wrapper. Native tasks use `GOWORK=off`;
browser builds select the workspace containing both modules and the pinned
Bubble Tea/clipboard forks. Dependency replace directives must be repeated in
the examples module (Go does not inherit them from the library).

The action status protocol caps decoded snapshots at 2,048 bytes. Advertising
all 18 individual 3D controls exceeded that limit and prevented startup; the
wrapper validates the snapshot before booba starts. Keep the browser bar to a
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
Add the two-import mapping above, the legacy cache regression and its fixed
release when available, and a multi-module consumer example covering workspace
selection, replace inheritance, action controls, and --no-index asset export.

Native Kitty, browser pixel rendering, live tile requests/CORS, and hardware GPU
behavior still need visual/manual acceptance; automated checks do not establish
those results. No sibling guide or dependency source was edited here.
