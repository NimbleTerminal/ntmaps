# ntcharts3d: measure presentation costs and assess renderer scratch reuse

Please investigate the next performance improvements after the texture-copy
fast paths. Follow this repository's AGENTS.md and required checks. Focus on
end-to-end presentation measurement and software-renderer lifetime, coordinating
with nt3d rather than duplicating its sampling work.

## Current evidence

Consumer: `../ntcharts-osm/examples/mapview3d` with ntcharts3d v0.3.1,
nt3d v0.2.0, and ntpicture v0.1.0. It renders a flat, opaque, unlit map texture,
512×512, with default 4× anisotropy. Published-release measurements on Apple M5
Max / Go 1.27.2:

- New texture: about 0.213 ms and 2 allocations.
- Fresh texture plus 640×320 render: about 14.3 ms and 39 allocations.
- Camera-only 640×320 snapshot: about 8.7 ms and 8 allocations.
- Camera frames allocate roughly eight bytes per output pixel: RGBA color plus
  float32 depth, about 1.56 MiB/frame at 640×320.

These are native Snapshot benchmarks. They omit ntpicture encoding, ordered
output, Bubble Tea rendering, terminal transport/display, and WASM scheduling.
They do not establish interactive FPS. More detail is in the consumer's
`examples/mapview3d/PERFORMANCE.md`.

## First: close the measurement gap

Build a reproducible presentation benchmark using actual chart Update/command
execution and ntpicture's completion signals. Do not just call Snapshot or
execute rendering commands without routing their results.

Measure camera motion and fresh texture replacement separately. Where supported,
cover glyph output, direct Kitty transport, and shared memory; include WASM
lifecycle/performance measurements if the existing harness supports them.
Record:

- Requested/completed/coalesced frames and actual output dimensions.
- Renderer, picture mode, quality adaptation, and transport actually selected.
- Rendering/preparation time versus encoding/output-completion time.
- Allocations and emitted protocol bytes.
- Warmup, frame trajectory, workload duration, and any slow-consumer behavior.

Prevent silent fallback from being mistaken for improved performance. Disable
or record adaptive resolution. Make clear that ntpicture PresentedMsg signals
ordered output/grid completion, not a terminal display acknowledgement. A sink
writer benchmark measures application-side costs, not emulator paint speed.
Use deterministic local image fixtures rather than live map downloads.

## Second: assess safe depth-scratch reuse

The current software path constructs an nt3d software renderer per call, while
the rasterizer allocates color and depth buffers each frame. Investigate whether
retaining a renderer and reusing private depth scratch produces a worthwhile
consumer improvement.

Before implementation, inspect nt3d's existing renderer/depth APIs and establish
ownership. Prefer retaining scratch that never escapes the renderer. Do not
reuse the returned image while ntpicture or callers may still read it. Some
rendering APIs expose depth data; do not overwrite caller-owned/retained results.
Snapshot can coexist with interactive rendering, so explicitly assess locking,
serialization, resizing, failure/fallback, and Close behavior before sharing a
persistent renderer.

If nt3d needs a change, provide its agent a precise API/ownership request and
coordinate who implements it. Avoid introducing a broad pooling abstraction or
public API without evidence. Benchmark both time and allocation reduction; low
allocation counts alone do not prove a responsiveness gain.

## Validation and coordination

nt3d has a separate task for sampling and encoded-color conversion. ntgpuimage
has a separate task for mipmap generation. Keep these effects distinguishable.
Do not lower default filtering quality or resolution to claim an optimization.

Run lifecycle, retained-frame, resize, cancellation, fallback, shutdown, race,
and pixel-equivalence tests as applicable. Include the mapview3d consumer and
normal chart workloads so a two-triangle optimization does not regress others.
Use temporary consumer modfiles/workspaces/copies; do not edit sibling working
trees, which may contain unpushed work.

Deliver a reproducible measurement harness and findings first, then any justified
implementation with tests and before/after data. State remaining gaps, required
upstream changes, and release/pin updates. Do not push or publish unless separately
authorized.
