# Texture allocation optimization handoff (historical)

**Integrated:** the demo now uses published ntcharts3d v0.3.1 and nt3d v0.2.0,
which include the fast paths below. These patches are retained as historical
evidence; do not apply them to the new releases. See
[the current performance report](../PERFORMANCE.md) for release measurements.

These patches were developed and tested in isolated copies of the published
modules, not the sibling working trees. At prototype time, no production dependency pins changed.

## Original changes requested from the libraries (now released)

1. **nt3d**, based on v0.1.0:
   [nt3d-texture-fastpath.patch](nt3d-texture-fastpath.patch). NewTexture copies
   NRGBA rows directly and opaque RGBA rows after a typed opacity check. The
   generic path remains for translucent RGBA and other image implementations.
   Copying respects bounds and row stride and never aliases the source.
2. **ntcharts3d**, based on v0.3.0:
   [ntcharts3d-texture-fastpath.patch](ntcharts3d-texture-fastpath.patch).
   NewTexture validates RGBA/NRGBA opacity using their Opaque methods rather
   than boxing each pixel through image.Image.At. Validation behavior remains
   unchanged. The full allocation win needs the nt3d change as well.

Each patch includes regression tests. Review/adapt against the current checkout
with `git apply --check /absolute/path/to/the.patch`, then apply it. Release an
nt3d patch version, update ntcharts3d's nt3d requirement, and release an
ntcharts3d patch version. No public API changes or ntpicture/booba changes are
needed for this optimization. This consumer can then upgrade its examples
module, regenerate notices, and rerun native/browser checks and benchmarks.

## Measured result

Apple M5 Max, Go 1.27.2, darwin/arm64, 2026-10-18. Same benchmark code and
512×512 texture, three runs of exactly 144 operations per case (two complete
camera rotations for replacement+render). Tests ran before timing, not during
it. Median wall time per operation:

| Case | Published | Both patches | Change |
| --- | ---: | ---: | ---: |
| Create texture | 5.642 ms | 0.203 ms | about 28× faster |
| Create texture allocations | 786,434 | 2 | per-pixel allocations removed |
| Create texture allocated bytes | 4,194,465 | 1,048,690 | about 75% less |
| Replace texture + render at 640×320 | 20.201 ms | 15.126 ms | about 25% less time |
| Replacement allocations | 786,471 | 39 | per-pixel allocations removed |
| Replacement allocated bytes | 7,232,940 | 4,087,212 | about 43% less |

Raw data: [before](benchmark-before.txt), [after](benchmark-after.txt).
This measures CPU rendering, not observed terminal FPS. Camera-only rendering
is not optimized by these patches; texture sampling and framebuffer allocation
remain future work. No filtering quality was reduced.

## Verification performed

- Full native race suites for the isolated nt3d and ntcharts3d root modules.
  The ntcharts3d suite used its published nt3d pin; consumer tests/benchmarks
  below selected both modified libraries together.
- Consumer mapview3d tests with both patches and the race detector.
- Browser mapview3d compilation with both patches and the pinned WASM forks.
- Pixel-byte comparisons of typed paths against the original generic path,
  including RGBA/NRGBA, transparency, nonzero bounds, cropped images with larger
  strides, and mutation of source pixels after constructing a texture.
- Opacity rejection within a subimage and acceptance when only pixels outside
  that subimage are transparent.

The broader library release gates and browser/native visual acceptance should
still run in their owning repositories. Existing GPU rendering code is unchanged.

## Local reproduction

The disposable prototype is at `/tmp/ntcharts-osm-texture-opt`. While it exists:

```sh
go -C examples test -modfile=/tmp/ntcharts-osm-texture-opt/consumer.mod \
  -run '^$' -bench '^BenchmarkMap3D(NewTexture|TextureReplacement)$' \
  -benchmem -benchtime=144x -count=3 ./mapview3d
```

Omitting `-modfile` now tests the upgraded published releases, not the old baseline. The modfile selects both modified
libraries without changing this repository's go.mod/go.sum.
