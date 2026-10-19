# Depth reuse consumer review

2026-10-18, Apple M5 Max, darwin/arm64. No correctness blocker found in review.

Combined current nt3d, ntcharts3d and ntgpuimage working trees passed:
`go test -race ../nt3d/... ../ntcharts3d/... ./examples/...` using a temporary workspace.
This includes the new ownership, retention, render/Close and Snapshot overlap tests.
Nested independent modules, GPU-enabled suites and browser execution were not rerun.

Controlled map benchmark: three sequential samples of 144 operations each,
640×320 software snapshots, anisotropy 4, deterministic 512×512 map texture.
Medians below; raw samples are in before.txt and after.txt. No concurrent benchmarks.

| Workload | Before ms/op | After ms/op | Before B/op | After B/op | Allocs before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Camera | 5.828 | 5.790 | 1,638,916 | 819,715 | 8 → 7 |
| Texture replacement | 7.311 | 7.123 | 4,087,324 | 3,268,122 | 39 → 38 |

The robust result is 819,200 fewer bytes and one fewer allocation per frame.
Small timing improvements in this short run do not establish a general speedup.
At 30 frames/s this avoids about 23.4 MiB/s of allocation, retaining 0.78125 MiB
of private depth at this size. Returned colors and public depth remain independent.

Baseline copies use nt3d HEAD 2111d0c and ntcharts3d HEAD f5ee68c production
renderer code, retaining the new explicit chart camera mapping for compatibility.
After uses their current uncommitted depth changes (tracked Go diffs archived here).
Both select the same local ntgpuimage 34fa60e and consumer sources. Production pins
are unchanged. Temporary workspaces and baseline copies are in
/private/tmp/osm-depth-review; these results do not compare against released pins.

nt3d retains at most 4 MiB of private depth, shrinks below one-quarter capacity,
and uses temporary scratch for larger requests. ntcharts3d owns its software
renderer separately from GPU lifetime and serializes access. Close releases scratch.
Review also checked frame validation, supersampling, fallback and result ownership.
The upstream reports include broader paired presentation and pixel-oracle results;
those are upstream evidence, not rerun measurements here.

Release dependency order remains ntgpuimage (for prior sampling gains), nt3d,
then ntcharts3d, followed by consumer pins. Both depth changes are still uncommitted.
