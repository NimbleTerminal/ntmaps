# Local ntpicture NRGBA fix: consumer benchmark

2026-10-18, Apple M5 Max, darwin/arm64. The local fix at ntpicture
`69a64dae94bff8a52f3b50bf014b952acfe86d68` removes the measured per-pixel allocation
hotspot and substantially improves GPU presentation in this consumer workload.
Production pins and sibling working trees were not modified.

## Matched results

Medians of three sequential samples. Packing uses 200 ms samples; GPU presentation
uses 144 operations per sample after warmup. Native WebGPU/Metal, 640×320,
anisotropy 4, direct Kitty zlib transport. The harness rejects renderer fallback,
resolution adaptation and unexpected transport. Camera moves in 5-degree steps;
texture replacement holds the camera fixed. These are different workloads.

| Workload | Before ms/op | After ms/op | Time reduction | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| NRGBA RGB packing | 1.669 | 0.141 | 91.6% | 204,801 → 1 |
| NRGBA strided subimage packing | 1.679 | 0.141 | 91.6% | 204,801 → 1 |
| GPU camera presentation | 3.204 | 1.356 | 57.7% | 206,005 → 1,203 |
| GPU texture replacement + presentation | 4.703 | 3.136 | 33.3% | 206,463 → 1,662 |

RGBA control packing stayed about 0.143 → 0.142 ms with one allocation. Generic
control packing stayed about 3.075 → 3.002 ms and 204,801 allocations. The new
branch improves NRGBA without changing the generic fallback's allocation behavior.

GPU camera presentation allocation volume fell from 3,738,137 to 2,918,055 B/op;
texture replacement from 7,459,288 to 6,639,364 B/op. Both avoid roughly 0.78 MiB
per operation. Camera output-stage time fell from 2.462 to 0.925 ms/frame, and
texture output-stage time from 2.264 to 0.693 ms/frame. Stage medians need not sum
to the total median. No before/after samples ran concurrently.

Raw samples: [packing before](packing-before.txt), [packing after](packing-after.txt),
[GPU before](gpu-before.txt), [GPU after](gpu-after.txt). These contain the full
sample ranges and renderer/transport assertions. Results are a new matched pair,
not a direct comparison against previous runs with older dependency trees.

## Validation

- Full root-module ntpicture race tests, ntcharts3d root race tests and this
  consumer's examples-module race tests passed with the frozen after workspace.
  See [validation.txt](validation.txt). This includes the new bounds, stride,
  ownership, wrapper and alpha/decoded-payload tests supplied by ntpicture.
- A separate 720-frame GPU camera allocation profile no longer contains
  `image.(*NRGBA).At`, even with the report node threshold set to zero.
  See [allocation profile](allocation-profile.txt), [profile run](profile-run.txt)
  and `after-mem.out`. This is sampled allocation evidence, supported by the exact
  focused benchmark count of one allocation. Profiles include initialization;
  shader compilation entries are not evidence of a per-frame bottleneck.
- The after implementation completed 1,440 camera operations, 20 texture
  replacements and repeated resizes among 320×160, 640×320 and 1280×640, without
  fallback or completion timeout. Mixed-size operation median was 1.36 ms,
  p95 4.16 ms, maximum 10.16 ms. Close completed with two goroutines and about
  5.43 MiB live Go heap. See [stress log](stress.txt). This short unpaced run is
  not a long-duration leak certification. Resize re-presentations are handled
  by the stress-only harness option documented in the earlier GPU review.

Actual terminal painting, browser execution, shared-memory consumption and full
upstream release matrices were not rerun. Presentation here means ordered
application output completion into a discard sink, not display acknowledgement
or visible FPS. The tests exclude tile networking and map composition.

## Sources and reproduction

Both ntpicture variants are disposable copies of the same local tree. Only
`kitty.go` differs: before restores the file from parent commit
`576fc07628a4a8940b548b8b32de51ea651cea16`; after uses the fixed file. Both retain
the same new tests/benchmarks and the earlier legacy cache fix. The unrelated
local MIGRATION.md edit does not affect execution.

nt3d and ntgpuimage were copied into `/private/tmp/osm-nrgba-review` before
benchmarking to isolate ongoing sibling work. Both variants use identical copies,
plus the archived ntcharts3d GPU-review harness. [sources.json](sources.json)
records revisions, working-tree status and Go source hashes. The current nt3d
snapshot includes ongoing Gaussian work, so these results do not isolate or
assess that feature; it is identical on both sides and the map has no Gaussians.

Temporary workspaces are `before.work` and `after.work` under that directory;
they select root, examples, the ntcharts3d review copy, frozen nt3d/ntgpuimage,
and the corresponding ntpicture copy. They leave all production pins unchanged.
The archived presentation and stress test sources accompany this report. Recreate
these source trees and workspaces if the temporary directory has been removed.

```sh
# Substitute before or after for VARIANT.
GOWORK=/private/tmp/osm-nrgba-review/VARIANT.work \
  go test /private/tmp/osm-nrgba-review/VARIANT -run '^$' \
  -bench '^BenchmarkKittyRGBPacking$/^640x320$' -benchtime=200ms -count=3 -benchmem
OSM_REVIEW_GPU=1 GOWORK=/private/tmp/osm-nrgba-review/VARIANT.work \
  go test -tags webgpu /private/tmp/osm-gpu-review/ntcharts3d -run '^$' \
  -bench '^BenchmarkPresentation$/^direct$/^(camera|texture)$' \
  -benchtime=144x -count=3 -benchmem
```

No correctness blocker surfaced in this consumer check. Select the published
ntpicture release when available, then rerun consumer integration with final
published dependencies. No commit or release was made here.
