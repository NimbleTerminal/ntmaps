# Eliminate per-pixel allocations in ntpicture's NRGBA Kitty encoding

Implement and measure an NRGBA fast path in ntpicture's Kitty RGB packing. Follow this repository's instructions and preserve unrelated/uncommitted changes. Do not commit, push, release, or change production dependency pins without separate authorization.

## Evidence and objective

The ntcharts-osm native GPU review found that rendering is fast, but presenting GPU images incurs approximately one allocation per pixel. ntgpuimage readback returns `*image.NRGBA`; ntpicture's `kittyRGBBytes` currently unwraps image views and handles `*image.RGBA` directly, but NRGBA takes the generic `img.At` / `color.NRGBAModel.Convert` path.

At 640×320, this creates roughly 204,800 avoidable allocations per frame. Measured camera presentation had about 206,024 allocations/op, versus 207 for software presentation. GPU snapshots alone had about 1,016 allocations/op. An allocation profile attributed 95.75% of sampled allocation objects to `image.(*NRGBA).At`, reached through `kittyRGBBytes` and Kitty zlib encoding. Profiles include initialization; use benchmark rows for per-operation counts.

GPU camera presentation took a median 3.162 ms/op: approximately 0.664 ms rendering and 2.461 ms in post-render preparation/encoding/output completion. These figures identify an allocation problem; they do not predict how much latency this fix will save.

Read the consumer evidence from this repository root:

- `../ntcharts-osm/examples/mapview3d/gpu-review/README.md`
- `../ntcharts-osm/examples/mapview3d/gpu-review/allocations-profile.txt`
- `../ntcharts-osm/examples/mapview3d/gpu-review/presentation-gpu.txt`
- `../ntcharts-osm/examples/mapview3d/gpu-review/presentation_bench_test.go.txt`
- `../ntcharts-osm/examples/mapview3d/gpu-review/sources.json`

First inspect current code and call sites; the working trees may have changed. The hotspot was observed with published ntpicture v0.1.0, and the same helper was present in the local sibling tree when reviewed.

## Implementation requirements

- Add a typed `*image.NRGBA` path in `kittyRGBBytes` that packs RGB directly from pixel storage, avoiding interface-based per-pixel color access/conversion.
- Preserve exact output bytes and row order. Account for nonzero/negative bounds, padded stride, and subimages whose pixel slice starts inside a larger backing image.
- Preserve `imageview.Unwrap` behavior and the generic fallback for arbitrary image implementations. Keep the existing RGBA path's behavior unchanged.
- Preserve the caller's opacity decision: RGB packing must not cause translucent images to be sent as opaque RGB. Keep straight-alpha semantics and the translucent RGBA encoding path intact, including transparent pixels with nonzero RGB values.
- Do not mutate or retain caller pixel storage. Returned packed bytes must remain independently owned across later calls and source mutations.
- Prefer a small local change. Do not add global pools, public APIs, transport changes, or GPU readback format changes to address this hotspot.

## Correctness tests

Use a reference implementation of the existing generic packing loop as an oracle. Compare exact bytes for deterministic opaque NRGBA images, including:

- A normal image, single-pixel and odd-width images, and supported empty bounds.
- Nonzero and negative origins, padded rows, and cropped subimages.
- Wrapped image views, plus a custom `image.Image` wrapper that forces the generic fallback.
- Existing RGBA behavior and generic image types.
- Output ownership: a later pack or source mutation cannot change an earlier returned byte slice.

Exercise actual Kitty encoding as well as the private helper. Verify that opaque NRGBA selects the expected RGB format and decodes to the same pixels. Verify translucent/transparent inputs retain the correct alpha-bearing format and values. Compare decoded payloads and protocol metadata; do not assume compressed byte streams must stay identical unless that is already a repository contract.

Run the relevant existing suites, race checks and repository-required validation. Include browser/WASM compilation or execution where supported, clearly distinguishing compilation from runtime testing.

## Performance validation

1. Add focused packing benchmarks for identical opaque RGBA and NRGBA data at 320×160, 640×320 and 1280×640. Include a strided subimage and a generic-image control. Construct fixtures outside the timed loop and report ns/op, B/op and allocs/op. Confirm NRGBA packing allocations no longer scale with pixel count; the independently owned output allocation is expected.
2. Compare before/after using the same compiler, fixtures and dependency versions. Run repeated samples sequentially. Report medians/ranges, including any regressions; avoid timing assertions in tests.
3. Rerun the consumer GPU presentation benchmark if native GPU access is available. Use disposable copies/workspaces to select before/after ntpicture with otherwise identical local nt3d, ntcharts3d and ntgpuimage sources. Preserve the archived harness's GPU assertion, 640×320 raster, anisotropy 4, direct Kitty transport and zlib format. Do not accept software fallback or reduced resolution as a valid GPU sample.
4. Measure camera and texture-replacement presentation, keeping each workload's existing camera behavior. Report total time, rendering and output-stage times, B/op and allocs/op. Profile again to confirm removal of the `NRGBA.At` allocation hotspot. Remaining driver, compression and output allocations are expected; do not promise zero-allocation presentation.

The archived presentation harness lives inside the ntcharts3d package and requires a disposable copy of that module; it is not a standalone program. Its README documents the commands and temporary-workspace setup. If reconstructing the exact historical sources is impractical, capture a new matched before/after pair and identify those sources explicitly.

`PresentedMsg` measures ordered application output completion, not terminal display acknowledgement. The discard-sink harness excludes terminal painting and real output backpressure. Do not report these timings as visible FPS. If GPU or browser execution is unavailable, report the gap and complete the focused packing/encoding comparison without substituting a different workload.

## Deliverables

- The minimal implementation and correctness tests.
- Reproducible benchmarks and a short before/after report with raw results, source identifiers and executed validation commands.
- Confirmation that decoded pixels, alpha handling, ownership and transport behavior remain unchanged.
- Any remaining integration/release requirements. Leave production pins unchanged so the consumer can select a published release later.
