# Native GPU map performance review

2026-10-18, Apple M5 Max, darwin/arm64. Local nt3d, ntcharts3d and ntgpuimage
working trees, including depth reuse; published ntpicture v0.1.0. Production
pins and sibling repositories were not modified. Sources are identified in
[sources.json](sources.json). The allocation hotspot below also exists in the
current sibling ntpicture `kitty.go`.

## Findings

Native WebGPU worked without fallback. The driver profile contains Metal backend
calls. `TestGPUSnapshot` passed, and all GPU benchmarks reject renderer fallback.
The demo defaults to software; request GPU with `-renderer gpu`. Native backends
are registered without a build tag. `webgpu` selects opt-in tests/benchmarks.
The earlier README instruction that the tag enables native GPU was incorrect.

At fixed 640×320, anisotropy 4, these are medians of three sequential samples of
144 operations. Warmup and shader initialization are excluded from benchmark
timing. The map fixture is our deterministic 512×512 texture, with camera rotation
in 5-degree steps. Snapshot replacement also rotates; presentation replacement
holds the camera fixed, so those rows are different workloads.

| Workload | Software ms/op | GPU ms/op |
| --- | ---: | ---: |
| Camera snapshot | 5.790 | 0.380 |
| Texture replacement + snapshot | 7.123 | 2.367 |
| Camera + direct Kitty presentation completion | 6.854 | 3.162 |
| Texture replacement + direct Kitty presentation completion | 7.957 | 4.762 |

[GPU snapshot samples](snapshot-gpu.txt), [software snapshot samples](snapshot-software.txt),
[GPU presentation samples](presentation-gpu.txt), [software presentation samples](presentation-software.txt).
Software snapshots come from the immediately preceding depth-review run with the
same local dependency trees. Presentation comparisons ran sequentially in this run.

GPU camera presentation spends about 0.664 ms rendering and 2.461 ms in the
post-render output stage, versus 5.867 ms and 0.938 ms for software (stage medians).
This output stage includes image preparation, encoding and command delivery;
it is not just OS write time. GPU/software rasterization produces different
pixel data and compressed payload sizes; we did not assert byte-identical images.

## Concrete next optimization: ntpicture opaque NRGBA conversion

GPU readback produces `*image.NRGBA`. `ntpicture.kittyRGBBytes` has a typed RGBA
fast path but falls back to `img.At` plus `color.NRGBAModel.Convert` for NRGBA.
That creates approximately one allocation for each of the 204,800 pixels.
The allocation profile attributes 95.75% of sampled allocation objects to
`image.(*NRGBA).At`, reached through `kittyRGBBytes` and the zlib Kitty encoder.
Profiles include initialization; exact counts should come from benchmark rows.

Camera presentation has approximately 206,024 allocations/op on GPU, versus
207 on software. GPU snapshots alone have about 1,016 allocations/op, so the
per-pixel presentation conversion is a much larger target than driver wrappers.

Recommended upstream work: add a typed NRGBA RGB-packing path in ntpicture,
preserving output bytes, imageview unwrapping, nonzero bounds and subimage stride.
Keep opacity/alpha semantics and generic-image fallback intact. Test against the
existing generic conversion oracle and benchmark both packing alone and this GPU
presentation workload. Do not change GPU readback to premultiplied RGBA solely
to work around the consumer's missing fast path. No fix or projected speedup is
claimed here; encoding/compression still costs time after allocations are removed.

See [allocation profile](allocations-profile.txt), [CPU profile](cpu-profile.txt),
and the raw `mem.out`/`cpu.out`. CPU samples contain substantial runtime/native
synchronization; their percentages are not a GPU hardware utilization measure.
The profile run is separate from the unprofiled timings in the table.

## Rotation, texture updates and resizing

The direct-output stress test completed 1,440 camera operations (20 rotations)
and 20 fresh texture replacements, switching among 640×320, 1280×640 and 320×160.
It asserts GPU mode and the expected raster and rejects adaptive-quality changes.
No error, fallback, or completion timeout occurred. This is a short, roughly
8-second unpaced stress run, not a long-duration leak certification.

Mixed-size camera-operation latencies: median 3.27 ms, p95 10.39 ms, maximum
22.46 ms. Operations at texture-update points include that additional work.
These percentiles are not fixed-resolution camera latency percentiles.

RSS rose from about 52 MiB at warmup to 110 MiB after the first large resize,
then stayed around 110–112 MiB. Post-GC live Go heap varied with frame size and
retirement timing; same-size later samples were about 9.5 MiB. After Dispose/Close
it fell to 5.45 MiB, with two goroutines remaining. RSS need not return immediately
after Close and does not measure GPU-only memory. See [stress log](stress.txt).

The original presentation harness assumed every completion followed a new render.
Resizing can re-present a retained image, so that assumption initially failed.
The archived stress-only option accounts for those completions; regular benchmark
runs keep strict matching. This was a harness limitation, not a demonstrated
renderer failure. The final stress log includes re-presentations in its completion
counts. No timing claim uses the synthetic start timestamp for those completions.

## Scope and remaining interactive check

These tests execute the real chart command/Update/render/ntpicture pipeline with
ordered output to a discard sink, using the same map geometry/camera as the demo.
They exclude tile networking, live map composition, Bubble Tea screen diffing,
OS terminal backpressure and actual terminal painting. They do not establish
interactive display FPS, browser performance, or shared-memory consumption.
A native demo binary was built at `/private/tmp/osm-gpu-review/mapview3d`.

An attempt to inspect the demo in Ghostty was blocked by the computer-use tool:
“Computer Use is not allowed to use the app 'com.mitchellh.ghostty' for safety reasons.”
Consequently the real terminal visual check remains pending. To try the tested
local build manually:

```sh
/private/tmp/osm-gpu-review/mapview3d -renderer gpu -medium direct
```

## Reproduction

The committed-source-compatible GPU snapshot benchmark is in
`../gpu_benchmark_test.go`; `task bench-mapview3d-gpu` uses the selected module
versions. This run selected local libraries with a temporary workspace instead
of changing production pins. To reproduce the reported local run, reconstruct
the recorded source trees and use root, examples, nt3d, ntcharts3d and ntgpuimage
as workspace modules.

Presentation/stress code is archived as `presentation_bench_test.go.txt` and
`review_stress_test.go.txt`. Put these into a disposable copy of the recorded
ntcharts3d tree. Select that copy in the workspace. Commands used:

```sh
# OSM_REVIEW_GPU omitted selects software in the archived presentation harness.
OSM_REVIEW_GPU=1 GOWORK=/private/tmp/osm-gpu-review/local.work \
  go test -tags webgpu /private/tmp/osm-gpu-review/ntcharts3d -run '^$' \
  -bench '^BenchmarkPresentation$/^direct$/^(camera|texture)$' \
  -benchtime=144x -count=3 -benchmem
OSM_REVIEW_GPU=1 GOWORK=/private/tmp/osm-gpu-review/local.work \
  go test -tags webgpu /private/tmp/osm-gpu-review/ntcharts3d \
  -run '^TestReviewGPUSustained$' -count=1 -v
```

The GPU stress suite is not race-instrumented; the combined depth-review race
suite passed separately. No commits, releases or production-pin changes were made.
