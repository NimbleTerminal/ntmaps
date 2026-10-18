# 3D map performance measurements

The demo now uses published ntcharts3d v0.3.1 and nt3d v0.2.0. See
[released optimization results](#released-optimization-results) below. The
original baseline is retained for comparison.

## Original baseline

Measured 2026-10-18 on Apple M5 Max, darwin/arm64, Go 1.27.2.
Dependencies: ntcharts3d v0.3.0, nt3d v0.1.0, ntpicture v0.1.0.

Run `task bench-mapview3d` from the repository root. The task runs each case
three times with a one-second benchmark duration. Raw results are in
[benchmark-baseline.txt](benchmark-baseline.txt); the harness is
[benchmark_test.go](benchmark_test.go).

## Scope

These are native software-renderer CPU measurements, not terminal FPS.
The harness uses a deterministic opaque 512×512 map-like image, matching the
map source's 64×32 cells at 8×16 pixels per cell. The same flat two-triangle
surface, hidden axes, camera tilt, perspective, and distance as the demo are
used. Camera azimuth advances in five-degree steps. Each case warms the first
frame before timing; texture replacement then uses fresh texture objects.

Snapshot runs the same software engine at a fixed pixel resolution without
interactive quality adaptation. Tile downloading, map composition, Kitty
encoding/transport, terminal painting, and WASM/GPU execution are excluded.
The output sizes are representative pixel workloads, not exact terminal sizes.
Results include harness validation overhead and are local samples, not latency
percentiles or a cross-machine performance guarantee.

## Results

Times are medians of three runs. Memory is allocated per operation, not retained
heap size. 4× anisotropy is the demo default; 1× still uses mipmapped filtering.

| Workload | Median ms/op | Range ms/op | MiB/op | Allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Camera, 320×160, 1× filtering | 1.61 | 1.61–1.64 | 0.39 | 8 |
| Camera, 320×160, 4× filtering | 2.25 | 2.24–2.25 | 0.39 | 8 |
| Camera, 640×320, 1× filtering | 6.31 | 6.27–6.36 | 1.56 | 8 |
| Camera, 640×320, 4× filtering | 9.05 | 8.68–9.22 | 1.56 | 8 |
| Camera, 1280×640, 1× filtering | 24.46 | 24.24–24.55 | 6.25 | 8 |
| Camera, 1280×640, 4× filtering | 28.37 | 26.84–28.51 | 6.25 | 8 |
| Fresh texture + camera/render, 640×320, 4× | 19.88 | 19.65–19.92 | 6.90 | 786,471 |
| Create 512×512 texture only | 5.59 | 5.55–5.68 | 4.00 | 786,434 |

At 1280×640, rendering alone exceeds a 60 Hz frame budget (16.7 ms). At
640×320 it leaves CPU headroom, but these measurements cannot establish actual
interactive FPS. Reducing filtering to 1× reduced median render cost by about
30% at 640×320; assess oblique-map image quality before changing the default.

## Profile findings and next optimization candidates

A separate three-second CPU profile of the 640×320/4× camera case attributed
about 54% cumulative CPU samples to nt3d texture footprint sampling; bilinear
sampling alone accounted for about 41%. Per-pixel math (including logarithms
and powers) is also prominent. Raster allocation is dominated by the RGBA
framebuffer and float32 depth buffer, approximately eight bytes per output
pixel per frame. Buffer reuse needs explicit image-ownership/lifetime design:
ntpicture can retain submitted frames asynchronously.

The texture-only allocation profile attributes essentially all allocated
objects to `image.(*RGBA).At` and `image/color.nrgbaModel`. Inspection confirms
that ntcharts3d first scans every pixel through At/RGBA to validate opacity;
nt3d then scans every pixel through At and NRGBAModel.Convert to copy it.
For RGBA input this produces approximately three small allocations per pixel:
512×512×3 = 786,432, plus the texture and backing storage allocations.

A promising upstream optimization is concrete RGBA/NRGBA pixel fast paths and
avoiding redundant opacity scans where the API permits it. Preserve opacity
validation, premultiplied-alpha handling, nonzero image bounds, strides, and
immutable texture ownership. Texture mipmaps are constructed lazily on use,
so the replacement+render case also captures costs absent from NewTexture alone.
No production rendering settings or dependency source were changed here.

To reproduce profiles without leaving test binaries in the repository:

```sh
go -C examples test -run '^$' \
  -bench '^BenchmarkMap3DCamera/640x320/aniso4$' -benchtime=3s \
  -cpuprofile=/tmp/map3d-cpu.pprof -memprofile=/tmp/map3d-mem.pprof \
  -o /tmp/map3d.test ./mapview3d
go tool pprof -top /tmp/map3d-cpu.pprof

go -C examples test -run '^$' -bench '^BenchmarkMap3DNewTexture$' \
  -benchtime=2s -memprofile=/tmp/map3d-texture.pprof \
  -o /tmp/map3d-texture.test ./mapview3d
go tool pprof -top -alloc_objects /tmp/map3d-texture.pprof
```

## Tested optimization prototype

The typed-image fast paths now have tested upstream patches and matched
before/after measurements in [upstream/README.md](upstream/README.md). They
reduce texture creation from 5.64 ms / 786,434 allocations to 0.203 ms / 2
allocations, and texture replacement plus rendering by about 25%. Those prototype changes are now available through the published dependency
pins; no local replacements are needed.

## Released optimization results

Upgraded to ntcharts3d v0.3.1 and nt3d v0.2.0 on 2026-10-18. Verified with
`go -C examples list -m`; neither library uses a local replacement. The same
M5 Max / Go 1.27.2 benchmark used three runs of exactly 144 operations per
case, matching the original optimization comparison. Figures are medians.

| Workload | Old published versions | New published versions | Improvement |
| --- | ---: | ---: | ---: |
| Create 512×512 texture | 5.642 ms | 0.213 ms | about 26.5× faster |
| Texture allocations/op | 786,434 | 2 | per-pixel allocations eliminated |
| Texture bytes/op | 4,194,465 | 1,048,706 | about 75% less |
| Texture replacement + 640×320 render | 20.201 ms | 14.290 ms | about 29% less time |
| Replacement allocations/op | 786,471 | 39 | per-pixel allocations eliminated |
| Replacement bytes/op | 7,232,940 | 4,087,292 | about 43% less |

[Released benchmark output](benchmark-released.txt). The older comparison is
in [upstream/benchmark-before.txt](upstream/benchmark-before.txt). These runs
were taken at different times on the same machine, so small timing differences
from the prototype should not be treated as a statistically established gain.
The allocation reduction is reproduced by the released versions.

```sh
go -C examples test -run '^$' \
  -bench '^BenchmarkMap3D(NewTexture|TextureReplacement)$' \
  -benchmem -benchtime=144x -count=3 ./mapview3d
```

`task ci` passed with the releases: module checks, race tests, vet, native and
WASM builds, generated-site validation, and both browser lifecycle smoke tests.
Dependency notices were regenerated. The earlier camera-only timings remain
historical; this release check reran texture creation/replacement workloads.

## Follow-up speed review: ntcharts3d v0.3.1 / nt3d v0.2.0

Fresh three-second CPU profiles on the same M5 Max measured 8.74 ms/op for
640×320 camera rendering and 14.32 ms/op for texture replacement. These are
profiled runs, not substitutes for the repeated unprofiled baseline above.
No production code or quality settings changed during this review.

Prioritized opportunities:

1. **nt3d bilinear/anisotropic sampling.** `texture.go:sampleLevelData`
   accounts for 44% cumulative camera-profile samples; the enclosing
   `sampleFootprint` accounts for 57% (these overlap, do not add them).
   Compute the two x/two y addresses once, specialize repeating versus clamped
   sampling, and consider unrolling the fixed four-tap/channel loops. Preserve
   accumulation order where possible and compare pixels against the current
   implementation, including normal/data textures, edge clamping, repetition,
   mip transitions, and oblique camera views. This is the highest CPU target.
2. **nt3d encoded-color conversion.** The non-linear-frame textured branch in
   `render_software.go` calls exact `encodeSRGB` for each sampled RGB channel;
   its conversion closure accounts for about 15% cumulative camera samples.
   nt3d already has an 8-bit lookup encoder for linear-frame output, but it
   cannot simply replace this intermediate float conversion: tint, lighting,
   blending, and rounding can change. A carefully bounded fast path for opaque,
   unlit, untinted output or a tested float approximation is a candidate.
   Do not globally switch frame color space to obtain a speedup.
3. **ntgpuimage mipmap generation on texture replacement.** `downsampleMip`
   accounts for about 16% cumulative replacement-profile samples. The map uses
   a power-of-two 512×512 texture, but mip construction executes the general
   area-overlap downsampler. A 2×2 reduction fast path for even dimensions is a
   good candidate; preserve sRGB/alpha-weighted averaging and keep the general
   path for odd dimensions. Mipmap output must stay consistent for software/GPU.
4. **nt3d framebuffer allocation.** Color and depth buffers still allocate
   roughly eight bytes per output pixel per frame (1.56 MiB at 640×320; about
   47 MiB/s at 30 rendered frames/s). Reusing private depth scratch is lower-risk
   than reusing the returned image. ntcharts3d currently constructs a software
   renderer per render call, so persistent scratch would require coordination
   between ntcharts3d and nt3d. Returned images can outlive Render because
   ntpicture presents them asynchronously; do not recycle them prematurely.

Local review: `syncTexture` already skips unchanged image identities, so camera
movement does not rebuild textures. `composeLetterbox` already returns exact-size
images without another copy. The hidden map source stays in glyph mode and its
View is only used before a source image exists, avoiding ongoing hidden Kitty
encoding. A small cache of immutable textures could help revisiting cached map
positions, but not continuous panning through new positions, and would retain
mipmap memory. This is lower priority than the upstream hot paths.

These profiles exclude encoding, terminal transport/painting, tile I/O, and WASM.
Before buffer/lifecycle redesign, add an interactive presentation benchmark to
measure those costs and avoid optimizing only the snapshot path. Percentages
identify candidates; they are not measured speedups for proposed changes.

## Combined local pre-release round

See [performance-round2/README.md](performance-round2/README.md) for integrated
race/pixel/browser checks and matched snapshot, native presentation, and WASM
glyph timings using the three local library updates. Production pins remain
on the published releases.
