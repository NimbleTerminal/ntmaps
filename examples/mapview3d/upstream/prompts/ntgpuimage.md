# ntgpuimage: optimize power-of-two mipmap construction

Please investigate and optimize mipmap generation for frequently replaced map
textures. Follow this repository's AGENTS.md and required checks. Preserve
filtering quality and color/alpha semantics.

## Evidence

Consumer: `../ntcharts-osm/examples/mapview3d` using ntcharts3d v0.3.1,
nt3d v0.2.0, and ntgpuimage v0.3.0. Its opaque map texture is 512×512.
New texture construction is now about 0.213 ms with two allocations, but
texture replacement plus 640×320 software rendering takes about 14.3 ms.

A fresh native profile attributes about 16% cumulative replacement CPU samples
to `downsampleMip`. nt3d constructs mipmaps lazily on first texture use.
`MipChain` currently uses a general area-overlap downsampler even for the
power-of-two case. See `../ntcharts-osm/examples/mapview3d/PERFORMANCE.md`.
These numbers exclude terminal encoding/I/O and network fetching.

## Requested work

1. Reproduce timings for current `MipChain`, separating construction from
   later sampling. Record exact dependency versions and revisions.
2. Prototype a specialized 2×2 reduction when source/destination dimensions
   satisfy the exact relationship required by that path. Keep the general
   implementation for odd dimensions and other unsupported shapes. Handle the
   1×N/N×1 tail of a mip chain deliberately rather than assuming every level
   has four distinct source pixels.
3. Preserve sRGB decoding/encoding, alpha-weighted averaging, zero-alpha
   behavior, rounding, and unweighted/linear-data options. Preserve returned
   image types, ownership, dimensions, and any GPU upload contracts.
4. Investigate further color-conversion cost only after measuring the reduction
   fast path. Do not silently approximate sRGB conversion. Any non-identical
   result needs explicit error measurements and a separately reviewable proposal.

## Validation

Compare optimized output to the existing general algorithm, ideally exactly,
for every mip level. Cover:

- Opaque and translucent NRGBA input, fully transparent pixels with nonzero RGB,
  and representative other supported image types.
- SRGB and AlphaWeighted options independently and together.
- 1×1, 1×N, N×1, even/non-square, power-of-two, odd, and mixed-dimension chains.
- Nonzero bounds, subimage strides, and source immutability.
- Randomized fixtures and adversarial alpha/color rounding cases.

Benchmark full chains at 256², 512², and 1024² plus odd/non-square controls.
Report ns/op, bytes/op, and allocations. Then verify the consumer's actual
texture-replacement benchmark with your version selected explicitly; creation
alone does not exercise lazy mipmaps. Repeat runs without concurrent benchmarks.
Run repository checks and software/GPU mip parity checks where available;
state unavailable hardware checks rather than claiming them passed.

Test consumers with temporary copies/modfiles/workspaces. Do not mutate sibling
working trees or discard their unpushed changes.

## Coordination and deliverable

nt3d is independently optimizing texture sampling and encoded-color conversion.
Avoid changing mip semantics underneath it. ntcharts3d is assessing presentation
costs and renderer scratch ownership; neither task requires a new mip API unless
you find a concrete need.

Deliver a focused implementation, correctness tests, before/after isolated and
consumer measurements, and release guidance. Keep the consumer's quality
settings unchanged. Do not push or publish unless separately authorized.
