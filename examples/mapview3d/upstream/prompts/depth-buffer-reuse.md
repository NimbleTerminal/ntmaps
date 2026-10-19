# Safe depth-buffer reuse across nt3d and ntcharts3d

Implement and measure safe depth-buffer reuse across nt3d and ntcharts3d.

Coordinate ownership between the two projects. Follow each repository’s instructions and preserve unrelated/uncommitted work. Do not push or release without separate authorization.

## Problem

Software rendering currently allocates a fresh RGBA image and float32 depth buffer per frame. At 640×320, those buffers total about 1.56 MiB/frame. Reusing private depth storage could avoid 819,200 allocated bytes/frame after warmup—about 23.4 MiB/sec at 30 frames/sec.

This is an allocation opportunity, not a proven latency improvement. Clearing depth and allocating an independently owned output image remain necessary.

The ntcharts3d assessment is in:

- `../ntcharts3d/performance/README.md`

Consumer benchmarks and integrated results:

- `../ntcharts-osm/examples/mapview3d/PERFORMANCE.md`
- `../ntcharts-osm/examples/mapview3d/performance-round2/README.md`

First inspect current implementations; these repositories may have changed since that assessment.

## nt3d responsibilities

- Add reusable private depth scratch to the software renderer, preferably without introducing a new public API or global pool.
- Keep returned color images independently owned.
- Preserve independently owned depth results from APIs that expose them, including RenderSoftware/SoftwareResult.Depth. Never overwrite previously returned results.
- Reset every active depth element before each render, including equal-area dimension changes.
- Define bounded retention/growth and Close behavior. Large snapshots must not accidentally retain excessive scratch indefinitely.
- Preserve supersampling, overlays, clipping, transparency, and invalid-frame behavior.
- Explicitly document Render/Close concurrency semantics.

## ntcharts3d responsibilities

- Retain a dedicated software renderer so scratch survives between frames.
- Keep its lifetime independent of the GPU renderer and GPU fallback cleanup.
- Coordinate Snapshot, scheduled interactive rendering, and Close safely. Snapshot frame construction follows the model’s event-loop contract, but rendering may overlap an already-running render command.
- Serialize access where required. Do not hold locks across callbacks or create shutdown deadlocks.
- Release resources on Close, make Close idempotent, and prevent queued work from using a closed renderer.
- Preserve existing cancellation, stale-frame rejection, and Dispose behavior.

## Validation

Add meaningful tests for:

- Pixel equivalence against the previous implementation.
- Previously returned images and exposed depth remaining unchanged.
- Repeated renders, size growth/shrink, and equal-area shape changes.
- Supersampling and representative maps, bars, lines, points, and transparency.
- Snapshot overlapping interactive rendering.
- GPU fallback, invalid frames, cancellation/stale jobs, repeated Close, and races.

Run required repository checks. Report unavailable GPU/browser checks explicitly.

## Measurement

Compare old and new code using identical dependency versions except for this change. Do not mix in sampling, mipmap, or quality-setting changes.

Measure:

- ns/op, B/op, and allocations/op.
- Steady-size camera motion and fresh-texture replacement.
- Resize-heavy workloads and a large snapshot followed by small interactive frames.
- Retained memory after large renders and after Close.
- Application-side presentation completion using ntcharts3d’s existing harness.

Keep raster size, anisotropy, renderer, and transport fixed or explicitly verified. Repeat sequential runs and report ranges. PresentedMsg is output completion, not terminal display acknowledgement.

Use temporary workspaces/modfiles or consumer copies for cross-project testing. Keep production dependency pins unchanged until coordinated releases exist.

Deliver the implementation, regression tests, measured allocation and latency effects, retention policy, and any release/pin changes needed. If latency does not improve, say so clearly and assess whether the allocation reduction justifies the added lifecycle complexity.
