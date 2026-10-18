# nt3d: optimize software sampling and color conversion

Please optimize the software renderer using the consumer evidence below. Work
in this repository and follow its AGENTS.md and required checks. Preserve visual
quality and public behavior; do not lower anisotropy, resolution, or precision
just to improve the benchmark.

## Evidence

Consumer: `../ntcharts-osm/examples/mapview3d`.
Published baseline: ntcharts3d v0.3.1, nt3d v0.2.0, Go 1.27.2, Apple M5 Max.
A flat, opaque, unlit two-triangle map uses a 512×512 texture with 4× anisotropy.
A fresh native CPU profile measured about 8.74 ms per 640×320 camera frame.

- `texture.go:sampleLevelData`: about 44% cumulative CPU samples.
- Enclosing `Texture.sampleFootprint`: about 57% cumulative samples. These
  percentages overlap; do not add them.
- The encoded-color conversion closure in `render_software.go`, which calls
  `encodeSRGB` on sampled RGB, accounts for about 15% cumulative samples.

The texture-construction fast paths shipped in v0.2.0 are already integrated.
Do not redo that work. These profiles exclude Kitty encoding, terminal I/O,
WASM execution, and tile fetching. Full evidence and benchmark commands are in
`../ntcharts-osm/examples/mapview3d/PERFORMANCE.md`.

## Work, in priority order

1. Profile the current checkout and reproduce the consumer benchmark before
   changing anything. Record exact revisions and whether the published pins or
   local code are running.
2. Optimize bilinear/anisotropic sampling first. Candidates include computing
   the two x and two y pixel addresses once, specializing clamped versus
   repeating addressing, and unrolling fixed four-tap/channel loops. Measure
   rather than assuming these changes help. Preserve accumulation order where
   feasible and avoid increasing allocations or per-texture memory without a
   demonstrated tradeoff.
3. Separately investigate the sampled-color conversion in non-linear frames.
   nt3d already has an 8-bit lookup encoder for linear-frame output, but that
   encoder is not a drop-in replacement for intermediate float conversion
   before tinting, lighting, and blending. Consider a guarded fast path for
   eligible opaque/unlit/untinted output. Do not globally change color space.
   If an approximation is proposed, quantify maximum error and changed pixels;
   explicitly distinguish it from a pixel-identical optimization.

Keep sampling and color-conversion changes independently reviewable so their
performance and correctness effects can be attributed separately.

## Correctness and performance evidence

Use the existing implementation as a test oracle where practical. Cover:

- Clamped and repeated addressing, negative/out-of-range UVs, edges and corners.
- One-pixel dimensions, non-square textures, mip transitions and oblique views.
- Anisotropy 1 and 4 plus supported extremes.
- Color versus linear data/normal textures, alpha/cutouts, tinting, lighting,
  fog, and blending for any affected rendering paths.
- Camera motion across multiple full rotations and renderer output comparisons.
- Immutable texture ownership and concurrent readers where supported.

Run required tests/race checks, and GPU/software parity checks if available.
Report skipped hardware checks accurately. Benchmark both isolated sampling
and the real consumer using equal camera trajectories, warmup policy, and
operation counts. Keep old/new runs sequential and repeat them; report ranges,
allocations, and absolute frame times. Reject complexity without a repeatable
consumer improvement.

Use temporary modfiles/workspaces or temporary consumer copies to test local
code. Do not edit or reset sibling working trees, whose changes may be unpushed.

## Coordination

ntgpuimage is receiving a separate task for mipmap construction. Keep this work
focused on sampling/conversion; preserve compatibility with its existing output.
ntcharts3d is receiving a task to measure presentation overhead and assess
persistent software-renderer/depth-scratch ownership. Coordinate any renderer
lifecycle API changes with that agent; do not introduce framebuffer pooling
incidentally in this task.

Deliver the implementation, regression tests, before/after data, remaining
bottlenecks, and the release/version update consumers will need. Do not publish
or push unless separately authorized.
