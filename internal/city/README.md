# Experimental city scaffolding

This package is deliberately internal while we learn what a geographic 3D API
should look like. It depends only on the Go standard library. The adapter into
ntcharts3d belongs to `examples/internal/cityview` (shared by the city development viewer and explorer),
so adding this helper does not add
GPU/chart dependencies to the public map widget.

The first pipeline is:

1. `Decode` OSM Map API JSON (node references) or full Overpass JSON geometry.
2. Preserve footprint/tag data in a `Dataset`, recording unsupported input counts.
3. Select heights through `HeightPolicy`, preserving `height` / `levels` /
   `default` provenance. Defaults are 3 meters per floor and 9 meters per building.
4. Project the neighborhood to east/north meters using local Web Mercator,
   corrected to ground scale at the origin. Z is height above flat ground.
5. `Build` clean simple rings, triangulate concave roofs, and extrude walls and
   caps with outward normals. Compilation is independent of camera updates.
6. Adapt immutable vertices/indices/roads into chart geometry in the viewer.

The scene retains geographic origin, height provenance, per-building IDs, stats
and issues rather than returning only an anonymous renderer mesh. `Scene.Scale`
and `Normalize` match uniform chart normalization so movement can remain in
meters. Building parts take precedence over containing outlines to avoid drawing
both volumes. No inference is made about missing parts.

## Deliberate first-slice limits

- Small neighborhoods, latitudes within ±85°, no antimeridian wrapping.
- Flat ground and flat roofs. Total tagged building height includes the roof;
  floor-derived estimates add roof height/levels when available.
- Closed simple way footprints only. Concave rings and either winding work;
  courtyard/multipolygon relations and their member ways are omitted, not filled
  with invented roofs. Open ways, missing nodes and bad rings are counted.
- Inconsistent base heights at/above the top reset to ground. Invalid numeric
  height tags fall through to floor estimates/defaults; colors expose that source.
- At most 2,000 buildings, 512 input points per way, 200,000 mesh triangles and
  16 MiB per source response. Road meshes are capped at 50,000 segments. Live requests additionally restrict neighborhood
  size and have a 25-second deadline. Oversized replies fail rather than truncate.
- `Client.Fetch` is one explicit request, with no background fetching, retries,
  streaming, global cache or automatic neighborhood fetch on camera movement.
- Source structs/meshes are caller-owned. Treat a scene as immutable once handed
  to the viewer. This is not a concurrent mutation API.

## What we need to learn before exporting anything

- A provider boundary for OSM/Overpass/vector tiles, persistent caching, request
  cancellation and eviction as the visible neighborhood changes.
- Multipolygon assembly, inner rings/courtyards, robust triangulation, and clear
  diagnostics for partial or malformed source data.
- Terrain elevations, roof forms, consistent building-part ownership and tags.
- Accurate registration of a raster basemap with the geographic mesh frame.
- Camera behavior near ground level, selection, clipping, collision/navigation,
  visible-area queries, and a distinction between orbit and free-look controls.
- Mesh chunking/LOD and a measured frame/memory budget on larger neighborhoods.
- Height-estimate policy and metadata consumers actually need in the public API.

The bundled neighborhood is a reproducible offline fixture; network tests use a
local HTTP server. Rendering/controls are checked separately in the examples
module. The prototype does not change `mapview`'s existing API or rendering.

## Sample-app boundaries

`examples/internal/cityview` owns the chart adapter, keyboard navigation, pane
sizing, and the bundled Flatiron source data. Its View is a pane; the standalone
host chooses AltScreen and the explorer composes it alongside a places list.
The current chart API has no general pause method: the wrapper suspends image
presentation through Wireframe mode, then restores the selected renderer. Hosts
continue routing runtime/cleanup messages to both image owners while one is hidden.

`examples/internal/neighborhood` owns optional native disk caching and a bounded
in-memory scene cache. It is an application concern, not part of the proposed
geographic library API. The explorer owns request cancellation/generation checks,
loading/error states, view selection, and mapping the camera target back to the
2D geographic center. Disk caches contain source data, not serialized GPU resources.
